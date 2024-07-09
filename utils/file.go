package utils

import (
	"bytes"
	"io/fs"
	"time"

	"github.com/picosh/go-rsync-receiver/rsync"
)

type SenderFile struct {
	// TODO: store relative to the root to conserve RAM
	Path    string
	WPath   string
	Regular bool
}

type ReceiverFile struct {
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
func (f *ReceiverFile) FileMode() fs.FileMode {
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
