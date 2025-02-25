package rsyncreceiver

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/rsyncopts"
	"github.com/picosh/go-rsync-receiver/rsyncsender"
	"github.com/picosh/go-rsync-receiver/rsyncwire"
	"github.com/picosh/go-rsync-receiver/utils"
)

func ClientRun(opts *rsyncopts.Options, conn io.ReadWriter, filesystem utils.FS, paths []string, negotiate bool) error {
	var err error

	crd, cwr := rsyncwire.CounterPair(conn, conn)

	const sessionChecksumSeed = 666

	c := &rsyncwire.Conn{
		Reader: crd,
		Writer: cwr,
	}

	if negotiate {
		remoteProtocol, err := c.ReadInt32()
		if err != nil {
			return err
		}
		log.Printf("remote protocol: %d", remoteProtocol)
		if err := c.WriteInt32(rsync.ProtocolVersion); err != nil {
			return err
		}
	}

	if err := c.WriteInt32(sessionChecksumSeed); err != nil {
		return err
	}

	// Switch to multiplexing protocol, but only for server-side transmissions.
	// Transmissions received from the client are not multiplexed.
	mpx := &rsyncwire.MultiplexWriter{Writer: c.Writer}
	c.Writer = mpx

	defer func() {
		if err != nil {
			mpx.WriteMsg(rsyncwire.MsgError, fmt.Appendf(nil, "gokr-rsync [receiver]: %v\n", err))
		}
	}()

	rt := &Transfer{
		Opts: &TransferOpts{
			DryRun: opts.DryRun(),

			DeleteMode:       opts.DeleteMode(),
			PreserveGid:      opts.PreserveGid(),
			PreserveUid:      opts.PreserveUid(),
			PreserveLinks:    opts.PreserveLinks(),
			PreservePerms:    opts.PreservePerms(),
			PreserveDevices:  opts.PreserveDevices(),
			PreserveSpecials: opts.PreserveSpecials(),
			PreserveTimes:    opts.PreserveMTimes(),
			// TODO: PreserveHardlinks: opts.PreserveHardlinks,
		},
		Dest: "/",
		// TODO: what is Env used for and can we get rid of it?
		Env: Osenv{
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
		},
		Conn: c,
		Seed: sessionChecksumSeed,

		files: filesystem,
	}

	if opts.DeleteMode() {
		// receive the exclusion list (openrsync’s is always empty)
		exclusionList, err := rsyncsender.RecvFilterList(c)
		if err != nil {
			return err
		}
		log.Printf("exclusion list read (entries: %d)", len(exclusionList.Filters))
	}

	// receive file list
	log.Printf("receiving file list")
	fileList, err := rt.ReceiveFileList()
	if err != nil {
		return err
	}
	log.Printf("received %d names", len(fileList))
	stats, err := rt.Do(c, fileList, true)
	if err != nil {
		return err
	}

	log.Printf("stats: %+v", stats)
	return nil
}
