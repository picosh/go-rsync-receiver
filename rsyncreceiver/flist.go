package rsyncreceiver

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/antoniomika/go-rsync-receiver/rsync"
	"github.com/antoniomika/go-rsync-receiver/utils"
)

// rsync/flist.c:flist_sort_and_clean
func sortFileList(fileList []*utils.ReceiverFile) {
	sort.Slice(fileList, func(i, j int) bool {
		return fileList[i].Name < fileList[j].Name
	})
}

// rsync/flist.c:receive_file_entry
func (rt *recvTransfer) receiveFileEntry(flags uint16, last *utils.ReceiverFile) (*utils.ReceiverFile, error) {
	f := &utils.ReceiverFile{}

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
func (rt *recvTransfer) receiveFileList() ([]*utils.ReceiverFile, error) {
	var lastFileEntry *utils.ReceiverFile
	var fileList []*utils.ReceiverFile
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
