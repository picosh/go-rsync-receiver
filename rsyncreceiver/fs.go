package rsyncreceiver

import (
	"io"
)

// File System: need to handle all type of files: regular, folder, symlink, etc
type FS interface {
	Put(fileName string, content io.Reader, fileSize int64, mTime int64, aTime int64) (written int64, err error)
}
