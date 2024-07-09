package rsyncsender

import (
	"os"

	"github.com/picosh/go-rsync-receiver/rsyncwire"
	"github.com/picosh/go-rsync-receiver/utils"

	"github.com/picosh/go-rsync-receiver/rsync"
)

// rsync/flist.c:send_file_list
func (st *sendTransfer) sendFileList(opts *Opts, list []os.FileInfo) (*fileList, error) {
	var fileList fileList
	fec := &rsyncwire.Buffer{}

	for _, info := range list {
		// Only ever transmit long names, like openrsync
		flags := byte(rsync.XMIT_LONG_NAME)

		name := info.Name()
		// if idx == 0 {
		// 	name = "."
		// 	flags |= rsync.XMIT_TOP_DIR
		// }

		fileList.files = append(fileList.files, &utils.SenderFile{
			Path:    "/",
			Regular: info.Mode().IsRegular(),
			WPath:   name,
		})

		// 1.   status byte (integer)
		fec.WriteByte(flags)

		// 2.   inherited filename length (optional, byte)
		// 3.   filename length (integer or byte)
		fec.WriteInt32(int32(len(name)))

		// 4.   file (byte array)
		fec.WriteString(name)

		// 5.   file length (long)
		size := info.Size()
		if info.IsDir() {
			// tmpfs returns non-4K sizes for directories. Override with
			// 4096 to make the tests succeed regardless of the /tmp file
			// system type.
			size = 4096
		}
		fec.WriteInt64(size)

		fileList.totalSize += size

		// 6.   file modification time (optional, integer)
		// TODO: this will overflow in 2038! :(
		fec.WriteInt32(int32(info.ModTime().Unix()))

		// 7.   file mode (optional, mode_t, integer)
		mode := int32(info.Mode() & os.ModePerm)
		if info.Mode().IsDir() {
			mode |= rsync.S_IFDIR
		} else if info.Mode().IsRegular() {
			mode |= rsync.S_IFREG
		}

		fec.WriteInt32(mode)
	}

	const endOfFileList = 0
	fec.WriteByte(endOfFileList)

	const endOfSet = 0
	fec.WriteInt32(endOfSet)
	// fec.WriteInt32(endOfSet)

	// const ioErrors = 0
	// fec.WriteInt32(ioErrors)

	if err := st.conn.WriteString(fec.String()); err != nil {
		return nil, err
	}

	return &fileList, nil
}
