package rsyncreceiver

import (
	"fmt"
	"io"
	"os"
	"time"

	"log"

	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/rsyncchecksum"
	"github.com/picosh/go-rsync-receiver/rsynccommon"
	"github.com/picosh/go-rsync-receiver/utils"
)

// rsync/generator.c:generate_files()
func (rt *Transfer) GenerateFiles(fileList []*utils.ReceiverFile) error {
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
	if err := rt.Conn.WriteInt32(-1); err != nil {
		return err
	}

	// TODO: re-do any files that failed
	phase++
	log.Printf("generateFiles phase=%d", phase)
	if err := rt.Conn.WriteInt32(-1); err != nil {
		return err
	}

	log.Printf("generateFiles finished")
	return nil
}

// rsync/generator.c:skip_file
func (rt *Transfer) skipFile(f *utils.ReceiverFile, st os.FileInfo) (bool, error) {
	if st.Size() != f.Length {
		return false, nil
	}

	// TODO: always checksum flag

	// TODO: size only

	// TODO: ignore times

	return modTimeEqual(st.ModTime(), f.ModTime), nil
}

func modTimeEqual(a, b time.Time) bool {
	a = a.Truncate(time.Second)
	b = b.Truncate(time.Second)
	log.Printf("comparing mtime: %v vs. %v", a, b)
	return a.Equal(b)
}

// rsync/generator.c:recv_generator
func (rt *Transfer) recvGenerator(idx int, f *utils.ReceiverFile) error {
	if rt.listOnly() {
		fmt.Fprintf(rt.Env.Stdout, "%s %11.0f %s %s\n",
			f.FileMode().String(),
			float64(f.Length), // TODO: rsync prints decimal separators
			f.ModTime.Format("2006/01/02 15:04:05"),
			f.Name)
		return nil
	}
	log.Printf("recv_generator(f=%+v)", f)

	st, in, err := rt.files.Read(&utils.SenderFile{Path: f.Name})

	if !f.FileMode().IsRegular() {
		// None of the Preserve* options is enabled, so just skip over
		// non-regular files.
		return nil
	}

	requestFullFile := func() error {
		log.Printf("requesting: %s", f.Name)
		if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
			return err
		}
		if rt.Opts.DryRun {
			return nil
		}
		var sh rsync.SumHead
		if err := sh.WriteTo(rt.Conn); err != nil {
			return err
		}
		return nil
	}

	if err != nil {
		log.Printf("failed to open %+v (%s), continuing: %v", st, f.Name, err)
		return requestFullFile()
	}

	skip, err := rt.skipFile(f, st)
	if err != nil {
		return err
	}

	if skip {
		log.Printf("skipping %+v", f)
		return nil
	}

	if rt.Opts.DryRun {
		if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
			return err
		}

		return nil
	}

	log.Printf("sending sums for: %s", f.Name)
	if err := rt.Conn.WriteInt32(int32(idx)); err != nil {
		return err
	}

	return rt.generateAndSendSums(in, st.Size())
}

// rsync/generator.c:generate_and_send_sums
func (rt *Transfer) generateAndSendSums(in utils.ReaderAtCloser, fileLen int64) error {
	sh := rsynccommon.SumSizesSqroot(fileLen)
	if err := sh.WriteTo(rt.Conn); err != nil {
		return err
	}
	buf := make([]byte, int(sh.BlockLength))
	remaining := fileLen
	for i := int32(0); i < sh.ChecksumCount; i++ {
		n1 := min(int64(sh.BlockLength), remaining)
		b := buf[:n1]
		if _, err := io.ReadFull(in, b); err != nil {
			return err
		}

		sum1 := rsyncchecksum.Checksum1(b)
		sum2 := rsyncchecksum.Checksum2(rt.Seed, b)
		if err := rt.Conn.WriteInt32(int32(sum1)); err != nil {
			return err
		}
		if _, err := rt.Conn.Writer.Write(sum2); err != nil {
			return err
		}
		remaining -= n1
	}
	return nil
}
