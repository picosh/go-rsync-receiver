package rsyncreceiver

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/antoniomika/go-rsync-receiver/rsync"
)

// rsync/flist.c:flist_sort_and_clean
func sortFileList(fileList []*file) {
	sort.Slice(fileList, func(i, j int) bool {
		return fileList[i].Name < fileList[j].Name
	})
}

type file struct {
	Name       string
	Length     int64
	ModTime    time.Time
	Mode       int32
	Uid        int32
	Gid        int32
	LinkTarget string
	Rdev       int32
	Buf        *bytes.Buffer
}

// FileMode converts from the Linux permission bits to Go’s permission bits.
func (f *file) FileMode() fs.FileMode {
	ret := fs.FileMode(f.Mode) & fs.ModePerm

	mode := f.Mode & rsync.S_IFMT
	switch mode {
	case rsync.S_IFCHR:
		ret |= fs.ModeCharDevice
	case rsync.S_IFBLK:
		ret |= fs.ModeDevice
	case rsync.S_IFIFO:
		ret |= fs.ModeNamedPipe
	case rsync.S_IFSOCK:
		ret |= fs.ModeSocket
	case rsync.S_IFLNK:
		ret |= fs.ModeSymlink
	case rsync.S_IFDIR:
		ret |= fs.ModeDir
	}

	return ret
}

// rsync/flist.c:receive_file_entry
func (rt *recvTransfer) receiveFileEntry(flags uint16, last *file) (*file, error) {
	f := &file{}

	var l1 int
	if flags&rsync.XMIT_SAME_NAME != 0 {
		l, err := rt.conn.ReadByte()
		if err != nil {
			return nil, err
		}
		l1 = int(l)
	}

	var l2 int
	if flags&rsync.XMIT_LONG_NAME != 0 {
		l, err := rt.conn.ReadInt32()
		if err != nil {
			return nil, err
		}
		l2 = int(l)
	} else {
		l, err := rt.conn.ReadByte()
		if err != nil {
			return nil, err
		}
		l2 = int(l)
	}
	// linux/limits.h
	const PATH_MAX = 4096
	if l2 >= PATH_MAX-l1 {
		const lastname = ""
		return nil, fmt.Errorf("overflow: flags=0x%x l1=%d l2=%d lastname=%s",
			flags, l1, l2, lastname)
	}
	b := make([]byte, l1+l2)
	readb := b
	if l1 > 0 && last != nil {
		copy(b, []byte(last.Name))
		readb = b[l1:]
	}
	if _, err := io.ReadFull(rt.conn.Reader, readb); err != nil {
		return nil, err
	}
	// TODO: does rsync’s clean_fname() and sanitize_path() combination do
	// anything more than Go’s filepath.Clean()?
	f.Name = filepath.Clean(string(b))

	length, err := rt.conn.ReadInt64()
	if err != nil {
		return nil, err
	}
	f.Length = length

	if flags&rsync.XMIT_SAME_TIME != 0 && last != nil {
		f.ModTime = last.ModTime
	} else {
		modTime, err := rt.conn.ReadInt32()
		if err != nil {
			return nil, err
		}
		f.ModTime = time.Unix(int64(modTime), 0)
	}

	if flags&rsync.XMIT_SAME_MODE != 0 && last != nil {
		f.Mode = last.Mode
	} else {
		mode, err := rt.conn.ReadInt32()
		if err != nil {
			return nil, err
		}
		f.Mode = mode
	}

	return f, nil
}

// rsync/flist.c:recv_file_list
func (rt *recvTransfer) receiveFileList() ([]*file, error) {
	var lastFileEntry *file
	var fileList []*file
	for {
		b, err := rt.conn.ReadByte()
		if err != nil {
			return nil, err
		}

		if b == 0 {
			break
		}
		flags := uint16(b)

		f, err := rt.receiveFileEntry(flags, lastFileEntry)
		if err != nil {
			return nil, err
		}

		lastFileEntry = f
		fileList = append(fileList, f)
	}
	return fileList, nil
}
