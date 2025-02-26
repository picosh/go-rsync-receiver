package rsyncsender

import (
	"fmt"
	"io"
	"log"

	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/rsyncopts"
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
			mpx.WriteMsg(rsyncwire.MsgError, fmt.Appendf(nil, "gokr-rsync [sender]: %v\n", err))
		}
	}()

	st := &Transfer{
		Opts:  opts,
		Conn:  c,
		Seed:  sessionChecksumSeed,
		files: filesystem,
	}
	// receive the exclusion list (openrsync’s is always empty)
	exclusionList, err := RecvFilterList(st.Conn)
	if err != nil {
		return err
	}
	log.Printf("exclusion list read (entries: %d)", len(exclusionList.Filters))

	stats, err := st.Do(crd, cwr, paths, exclusionList)
	if err != nil {
		return err
	}

	log.Printf("handleConnSender done. stats: %+v", stats)

	return err
}
