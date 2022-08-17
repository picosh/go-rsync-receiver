package rsyncreceiver

import (
	"errors"
	"log"

	"github.com/antoniomika/go-rsync-receiver/rsync"
)

// rsync/generator.c:generate_files()
func (rt *recvTransfer) generateFiles(fileList []*file) error {
	phase := 0
	for idx, f := range fileList {
		// TODO: use a copy of f with .Mode |= S_IWUSR for directories, so
		// that we can create files within all directories.

		if err := rt.recvGenerator(idx, f); err != nil {
			return err
		}
	}
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.conn.WriteInt32(-1); err != nil {
		return err
	}

	log.Printf("generateFiles finished")
	return nil
}

// rsync/generator.c:recv_generator
func (rt *recvTransfer) recvGenerator(idx int, f *file) error {
	log.Printf("recv_generator(f=%+v)", f)

	if !f.FileMode().IsRegular() {
		// None of the Preserve* options is enabled, so just skip over
		// non-regular files.
		return errors.New("unsupported file types")
	}

	log.Printf("requesting: %s", f.Name)
	if err := rt.conn.WriteInt32(int32(idx)); err != nil {
		return err
	}

	var sh rsync.SumHead
	if err := sh.WriteTo(rt.conn); err != nil {
		return err
	}
	return nil
}
