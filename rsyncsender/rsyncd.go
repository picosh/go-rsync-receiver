package rsyncsender

import (
	"bufio"
	"fmt"
	"io"
	"sort"

	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/rsyncwire"
	"github.com/picosh/go-rsync-receiver/utils"
)

type sendTransfer struct {
	// state
	conn       *rsyncwire.Conn
	seed       int32
	lastMatch  int64
	filesystem utils.FS
}

type Module struct {
	Name string   `toml:"name"`
	Path string   `toml:"path"`
	ACL  []string `toml:"acl"`
}

// Option specifies the server options.
type Option interface {
}

type fileList struct {
	totalSize int64
	files     []*utils.SenderFile
}

// rsync/rsync.h defines chunkSize as 32 * 1024, but increasing it to 256K
// increases throughput with “tridge” rsync as client by 50 Mbit/s.
const chunkSize = 32 * 1024

type target struct {
	index int32
	tag   uint16
}

type countingReader struct {
	r    io.Reader
	read int64
}

func (r *countingReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.read += int64(n)
	return n, err
}

type countingWriter struct {
	w       io.Writer
	written int64
}

func (w *countingWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.written += int64(n)
	return n, err
}

func CounterPair(r io.Reader, w io.Writer) (*countingReader, *countingWriter) {
	crd := &countingReader{r: r}
	cwr := &countingWriter{w: w}
	return crd, cwr
}

func ClientRun(opts *Opts, conn io.ReadWriter, filesystem utils.FS, path string, negotiate bool) error {
	var err error

	crd := &countingReader{r: conn}
	cwr := &countingWriter{w: conn}

	rd := bufio.NewReader(crd)

	c := &rsyncwire.Conn{
		Reader: rd,
		Writer: cwr,
	}

	if negotiate {
		_, err = c.ReadInt32()
		if err != nil {
			return err
		}

		if err = c.WriteInt32(rsync.ProtocolVersion); err != nil {
			return err
		}
	}

	var seed int32 = 0

	if err := c.WriteInt32(seed); err != nil {
		return err
	}

	mpx := &rsyncwire.MultiplexWriter{Writer: c.Writer}
	c.Writer = mpx

	defer func() {
		if err != nil {
			mpx.WriteMsg(rsyncwire.MsgError, []byte(fmt.Sprintf("%v\n", err)))
		}
	}()

	st := &sendTransfer{
		conn:       c,
		seed:       seed,
		filesystem: filesystem,
	}

	// receive the exclusion list (openrsync’s is always empty)
	const exclusionListEnd = 0
	got, err := c.ReadInt32()
	if err != nil {
		return err
	}

	if want := int32(exclusionListEnd); got != want {
		return fmt.Errorf("protocol error: non-empty exclusion list received")
	}

	files, err := filesystem.List(path)
	if err != nil {
		return err
	}

	// send file list
	fileList, err := st.sendFileList(opts, files)
	if err != nil {
		return err
	}

	// Sort the file list. The client sorts, so we need to sort, too (in the
	// same way!), otherwise our indices do not match what the client will
	// request.
	sort.Slice(fileList.files, func(i, j int) bool {
		return fileList.files[i].WPath < fileList.files[j].WPath
	})

	if err := st.sendFiles(fileList); err != nil {
		return err
	}

	// send statistics:
	// total bytes read (from network connection)
	if err := c.WriteInt64(crd.read); err != nil {
		return err
	}
	// total bytes written (to network connection)
	if err := c.WriteInt64(cwr.written); err != nil {
		return err
	}
	// total size of files
	if err := c.WriteInt64(fileList.totalSize); err != nil {
		return err
	}

	finish, err := c.ReadInt32()
	if err != nil {
		return err
	}
	if finish != -1 {
		return fmt.Errorf("protocol error: expected final -1, got %d", finish)
	}

	return nil
}
