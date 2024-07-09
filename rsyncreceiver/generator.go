package rsyncreceiver

import (
	"errors"

	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/utils"
)

// rsync/generator.c:generate_files()
func (rt *recvTransfer) generateFiles(fileList []*utils.ReceiverFile) error {
	phase := 0
	for idx, f := range fileList {
		// TODO: use a copy of f with .Mode |= S_IWUSR for directories, so
		// that we can create files within all directories.
		if rt.files != nil && rt.files.Skip(f) {
			continue
		}

		if err := rt.recvGenerator(idx, f); err != nil {
			return err
		}
	}
	phase++
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	return nil
}

// rsync/generator.c:recv_generator
func (rt *recvTransfer) recvGenerator(idx int, f *utils.ReceiverFile) error {
	if !f.FileMode().IsRegular() {
		// None of the Preserve* options is enabled, so just skip over
		// non-regular files.
		return errors.New("unsupported file types")
	}

	if err := rt.conn.WriteInt32(int32(idx)); err != nil {
		return err
	}

	var sh rsync.SumHead
	if err := sh.WriteTo(rt.conn); err != nil {
		return err
	}
	return nil
}
