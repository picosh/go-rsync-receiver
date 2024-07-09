package rsyncreceiver

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"log"

	"github.com/google/shlex"
	"github.com/picosh/go-rsync-receiver/rsync"
	"github.com/picosh/go-rsync-receiver/rsyncwire"
	"github.com/picosh/go-rsync-receiver/utils"
)

type recvTransfer struct {
	// config
	opts  *Opts
	files utils.FS

	// state
	conn *rsyncwire.Conn
	seed int32
}

type Stats struct {
	Read    int64 // total bytes read (from network connection)
	Written int64 // total bytes written (to network connection)
	Size    int64 // total size of files
}

// parseHostspec returns the [USER@]HOST part of the string
//
// rsync/options.c:parse_hostspec
func parseHostspec(src string, parsingURL bool) (host, path string, port int, _ error) {
	var userlen int
	var hostlen int
	var hoststart int
	i := 0
	for ; i <= len(src); i++ {
		if i == len(src) {
			if !parsingURL {
				return "", "", 0, fmt.Errorf("ran out of string")
			}
			if hostlen == 0 {
				hostlen = len(src[hoststart:])
			}
			break
		}

		s := src[i]
		if s == ':' || s == '/' {
			if hostlen == 0 {
				hostlen = len(src[hoststart:i])
			}
			i++
			if s == '/' {
				if !parsingURL {
					return "", "", 0, fmt.Errorf("/, but not parsing URL")
				}
			} else if s == ':' && parsingURL {
				rest := src[i:]
				digits := ""
				for _, s := range rest {
					if !unicode.IsDigit(s) {
						break
					}
					digits += string(s)
				}
				if digits != "" {
					p, err := strconv.ParseInt(digits, 0, 64)
					if err != nil {
						return "", "", port, err
					}
					port = int(p)
					i += len(digits)
				}
				if i < len(src) && src[i] != '/' {
					return "", "", 0, fmt.Errorf("expected / or end, got %q", src[i:])
				}
				if i < len(src) {
					i++
				}
			}
			break
		}
		if s == '@' {
			userlen = i + 1
			hoststart = i + 1
		} else if s == '[' {
			if i != hoststart {
				return "", "", 0, fmt.Errorf("brackets not at host position")
			}
			hoststart++
			for i < len(src) && src[i] != ']' && src[i] != '/' {
				i++
			}
			hostlen = len(src[hoststart : i+1])
			if i == len(src) ||
				src[i] != ']' ||
				(i < len(src)-1 && src[i+1] != '/' && src[i+1] != ':') ||
				hostlen == 0 {
				return "", "", 0, fmt.Errorf("WTF")
			}
		}
	}
	if userlen > 0 {
		host = src[:userlen]
		hostlen += userlen
	}
	host += src[hoststart:hostlen]
	return host, src[i:], port, nil
}

// rsync/options.c:check_for_hostspec
func checkForHostspec(src string) (host, path string, port int, _ error) {
	if strings.HasPrefix(src, "rsync://") {
		var err error
		if host, path, port, err = parseHostspec(strings.TrimPrefix(src, "rsync://"), true); err == nil {
			if port == 0 {
				port = -1
			}
			return host, path, port, nil
		}
	}
	var err error
	host, path, port, err = parseHostspec(src, false)
	if err != nil {
		return host, path, port, err
	}
	if strings.HasPrefix(path, ":") {
		if port == 0 {
			port = -1
		}
		path = strings.TrimPrefix(path, ":")
		return host, path, port, nil
	}
	port = 0 // not a daemon-accessing spec
	return host, path, port, nil
}

// rsync/main.c:start_client
func rsyncMain(opts *Opts, sources []string, dest string, filesystem utils.FS) (*Stats, error) {
	log.Printf("dest: %q, sources: %q", dest, sources)
	log.Printf("opts: %+v", opts)
	for _, src := range sources {
		log.Printf("processing src=%s", src)
		daemonConnection := 0 // no daemon
		host, path, port, err := checkForHostspec(src)
		log.Printf("host=%q, path=%q, port=%d, err=%v", host, path, port, err)
		if err != nil {
			// TODO: source is local, check dest arg
			return nil, fmt.Errorf("push not yet implemented")
		} else {
			// source is remote
			if port != 0 {
				if opts.ShellCommand != "" {
					daemonConnection = 1 // daemon via remote shell
				} else {
					daemonConnection = -1 // daemon via socket
				}
			}
		}
		module := path
		if idx := strings.IndexByte(module, '/'); idx > -1 {
			module = module[:idx]
		}
		log.Printf("module=%q, path=%q", module, path)

		if daemonConnection < 0 {
			stats, err := socketClient(opts, src, dest, filesystem)
			if err != nil {
				return nil, err
			}
			return stats, nil
		} else {
			machine := host
			user := ""
			if idx := strings.IndexByte(machine, '@'); idx > -1 {
				user = machine[:idx]
				machine = machine[idx+1:]
			}
			rc, wc, err := doCmd(opts, machine, user, path, daemonConnection)
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			defer wc.Close()
			conn := &readWriter{
				Reader: rc,
				Writer: wc,
			}
			negotiate := true
			if daemonConnection != 0 {
				if err := startInbandExchange(opts, conn, module, path); err != nil {
					return nil, err
				}
				negotiate = false // already done
			}
			stats, err := ClientRun(opts, conn, filesystem, negotiate)
			if err != nil {
				return nil, err
			}
			return stats, nil
		}
	}
	return nil, nil
}

type readWriter struct {
	io.Reader
	io.Writer
}

func (prw *readWriter) Read(p []byte) (n int, err error) {
	return prw.Reader.Read(p)
}

func (prw *readWriter) Write(p []byte) (n int, err error) {
	return prw.Writer.Write(p)
}

// rsync/main.c:do_cmd
func doCmd(opts *Opts, machine, user, path string, daemonConnection int) (io.ReadCloser, io.WriteCloser, error) {
	cmd := opts.ShellCommand
	if cmd == "" {
		cmd = "ssh"
		if e := os.Getenv("RSYNC_RSH"); e != "" {
			cmd = e
		}
	}

	// We use shlex.Split(), whereas rsync implements its own shell-style-like
	// parsing. The nuances likely don’t matter to any users, and if so, users
	// might prefer shell-style parsing.
	args, err := shlex.Split(cmd)
	if err != nil {
		return nil, nil, err
	}

	if user != "" && daemonConnection == 0 /* && !dashlset */ {
		args = append(args, "-l", user)
	}

	args = append(args, machine)

	args = append(args, "rsync") // TODO: flag

	if daemonConnection > 0 {
		args = append(args, "--server", "--daemon")
	} else {
		args = append(args, serverOptions(opts)...)
	}
	args = append(args, ".")

	if daemonConnection == 0 {
		args = append(args, path)
	}

	log.Printf("args: %q", args)

	ssh := exec.Command(args[0], args[1:]...)
	wc, err := ssh.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	rc, err := ssh.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	ssh.Stderr = os.Stderr
	if err := ssh.Start(); err != nil {
		return nil, nil, err
	}

	go func() {
		// TODO: correctly terminate the main process when the underlying SSH
		// process exits.
		if err := ssh.Wait(); err != nil {
			log.Printf("remote shell exited: %v", err)
		}
	}()

	return rc, wc, nil
}

// rsync/main.c:client_run
func ClientRun(opts *Opts, conn io.ReadWriter, filesystem utils.FS, negotiate bool) (*Stats, error) {
	c := &rsyncwire.Conn{
		Reader: conn,
		Writer: conn,
	}

	if negotiate {
		_, err := c.ReadInt32()
		if err != nil {
			return nil, err
		}

		if err := c.WriteInt32(rsync.ProtocolVersion); err != nil {
			return nil, err
		}
	}

	var seed int32 = 0

	rt := &recvTransfer{
		opts:  opts,
		conn:  c,
		seed:  seed,
		files: filesystem,
	}

	// TODO: implement support for exclusion, send exclusion list here
	const exclusionListEnd = 0
	if err := c.WriteInt32(exclusionListEnd); err != nil {
		return nil, err
	}

	fileList, err := rt.receiveFileList()
	if err != nil {
		return nil, err
	}

	sortFileList(fileList)

	_, err = c.ReadInt32()
	if err != nil {
		return nil, err
	}

	mrw := &rsyncwire.MultiplexWriter{
		Writer: conn,
	}

	wr := bufio.NewWriterSize(mrw, 32*1024)
	c.Writer = wr

	rt.generateFiles(fileList)

	wr.Flush()

	rt.recvFiles(fileList)

	// send final goodbye message
	if err := c.WriteInt32(-1); err != nil {
		return nil, err
	}

	wr.Flush()

	return nil, nil
}

// rsync/token.c:recvToken
func (rt *recvTransfer) recvToken() (token int32, data []byte, _ error) {
	var err error
	token, err = rt.conn.ReadInt32()
	if err != nil {
		return 0, nil, err
	}
	if token <= 0 {
		return token, nil, nil
	}
	data = make([]byte, int(token))
	if _, err := io.ReadFull(rt.conn.Reader, data); err != nil {
		return 0, nil, err
	}
	return token, data, nil
}

func Main(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (*Stats, error) {
	opts, opt := NewGetOpt()
	remaining, err := opt.Parse(args[1:])
	if opt.Called("help") {
		return nil, errors.New(opt.Help())
	}
	if err != nil {
		return nil, err
	}

	if opts.Archive {
		// --archive is -rlptgoD
		opts.Recurse = true       // -r
		opts.PreserveLinks = true // -l
		opts.PreservePerms = true // -p
		opts.PreserveTimes = true // -t
		opts.PreserveGid = true   // -g
		opts.PreserveUid = true   // -o
		opts.D = true             // -D
	}

	if opts.D {
		opts.PreserveDevices = true
		opts.PreserveSpecials = true
	}

	if len(remaining) == 0 {
		return nil, errors.New(opt.Help())
	}
	if len(remaining) == 1 {
		// Usages with just one SRC arg and no DEST arg list the source files
		// instead of copying.
		dest := ""
		sources := remaining
		return rsyncMain(opts, sources, dest, nil)
	}
	dest := remaining[len(remaining)-1]
	sources := remaining[:len(remaining)-1]
	return rsyncMain(opts, sources, dest, nil)
}
