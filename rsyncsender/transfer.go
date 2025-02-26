package rsyncsender

import (
	"io"

	"github.com/picosh/go-rsync-receiver/rsyncopts"
	"github.com/picosh/go-rsync-receiver/rsyncwire"
	"github.com/picosh/go-rsync-receiver/utils"
)

type Osenv struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// TransferOpts is a subset of Opts which is required for implementing a receiver.
type TransferOpts struct {
	Verbose bool
	DryRun  bool

	DeleteMode        bool
	PreserveGid       bool
	PreserveUid       bool
	PreserveLinks     bool
	PreservePerms     bool
	PreserveDevices   bool
	PreserveSpecials  bool
	PreserveTimes     bool
	PreserveHardlinks bool
}

type Transfer struct {
	// config
	// Opts *Opts
	Opts *rsyncopts.Options

	// state
	Conn      *rsyncwire.Conn
	Seed      int32
	lastMatch int64

	files utils.FS
}

//func (rt *Transfer) listOnly() bool { return rt.Dest == "" }
