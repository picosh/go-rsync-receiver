package utils

import (
	"io"
	"os"
)

// File System: need to handle all type of files: regular, folder, symlink, etc
type FS interface {
	Put(*ReceiverFile) (int64, error)
	Skip(*ReceiverFile) bool
	List(string) ([]os.FileInfo, error)
	Read(*SenderFile) (os.FileInfo, io.ReaderAt, error)
}
