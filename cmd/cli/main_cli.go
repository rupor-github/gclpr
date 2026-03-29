package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/sign"

	"github.com/rupor-github/gclpr/misc"
	"github.com/rupor-github/gclpr/server"
	"github.com/rupor-github/gclpr/util"
)

const (
	exitSuccess = iota
	_
	_
	_
	_
	_
	exitFlagParseError
	exitNoKeys
	exitRPCError
	exitHelp
)

type command int

const (
	cmdOpen command = iota + 1
	cmdCopy
	cmdPaste
	cmdServer
	cmdGenKey
	cmdOAuthWorker
)

func (c command) String() string {
	switch c {
	case cmdOpen:
		return "open url in server's default browser"
	case cmdCopy:
		return "send text to server clipboard"
	case cmdPaste:
		return "output server clipboard locally"
	case cmdServer:
		return "start server"
	case cmdGenKey:
		return "generate key pair for signing"
	case cmdOAuthWorker:
		return "run detached oauth tunnel worker"
	default:
		return fmt.Sprintf("bad command %d", c)
	}
}

var (
	aPort             int
	aLE               string
	aHelp             bool
	aDebug            bool
	aTunnel           bool
	aOAuth            bool
	aWorkerStatusAddr string
	aData             string
	aConnectTimeout   time.Duration
	aIOTimeout        time.Duration
	cli               = flag.NewFlagSet("gclpr", flag.ContinueOnError)

	reOpen  = regexp.MustCompile(`/?xdg-open$`)
	rePaste = regexp.MustCompile(`/?pbpaste$`)
	reCopy  = regexp.MustCompile(`/?pbcopy$`)
)

// getCommand determines the command from args, either by matching the binary name
// (aliased as xdg-open, pbpaste, pbcopy) or by parsing an explicit subcommand.
func getCommand(args []string) (cmd command, aliased bool, err error) {

	aliased = true

	switch {
	case reOpen.MatchString(args[0]):
		cmd = cmdOpen
		return
	case rePaste.MatchString(args[0]):
		cmd = cmdPaste
		return
	case reCopy.MatchString(args[0]):
		cmd = cmdCopy
		return
	default:
		aliased = false
	}

	for i, v := range args[1:] {
		switch v {
		case "open":
			cmd = cmdOpen
		case "paste":
			cmd = cmdPaste
		case "copy":
			cmd = cmdCopy
		case "server":
			cmd = cmdServer
		case "genkey":
			cmd = cmdGenKey
		case "internal-oauth-worker":
			cmd = cmdOAuthWorker
		default:
			continue
		}
		copy(args[i+1:], args[i+2:])
		args[len(args)-1] = ""
		return
	}

	cli.Usage()

	err = errors.New("unknown command")
	return
}

// processCommandLine parses the full command line, extracting the command and its arguments.
func processCommandLine(args []string) (cmd command, err error) {

	var aliased bool
	cmd, aliased, err = getCommand(args)
	if err != nil {
		return
	}
	if !aliased {
		args = args[:len(args)-1]
	}

	if err = cli.Parse(args[1:]); err != nil {
		return
	}
	if !aDebug && envDebugEnabled() {
		aDebug = true
	}

	if cmd == cmdPaste || cmd == cmdServer || cmd == cmdGenKey || cmd == cmdOAuthWorker {
		return
	}

	var arg string
	for 0 < cli.NArg() {
		arg = cli.Arg(0)
		if err = cli.Parse(cli.Args()[1:]); err != nil {
			return
		}
	}

	if aHelp {
		return
	}
	if aTunnel && aOAuth {
		return 0, errors.New("-tunnel and -oauth cannot be used together")
	}
	if aliased && cmd == cmdOpen && !aTunnel && !aOAuth {
		aOAuth = true
	}

	if len(arg) != 0 {
		aData = arg
	} else {
		var b []byte
		if b, err = io.ReadAll(os.Stdin); err != nil {
			return
		}
		aData = string(b)
	}
	return
}

func parseTunnelTarget(raw string) (*url.URL, error) {
	parsed, err := server.ParseTunnelURL(raw)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

type secConn struct {
	conn net.Conn
	br   *bufio.Reader
	hpk  [32]byte
	k    *[64]byte
}

func (sc *secConn) Read(p []byte) (n int, err error) {
	data, err := util.ReadFrame(sc.br)
	if err != nil {
		return 0, err
	}
	copy(p, data)
	return len(data), nil
}

func (sc *secConn) Write(p []byte) (n int, err error) {
	header := append(misc.Magic(), sc.hpk[:]...)
	out := sign.Sign(header, p, sc.k)
	if err = util.WriteFrame(sc.conn, out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (sc *secConn) Close() error {
	return sc.conn.Close()
}

// doRPC reads keys, connects to the server, and executes the given RPC operation.
func doRPC(home string, op func(*rpc.Client) error) error {

	pk, k, err := util.ReadKeys(home)
	if err != nil {
		return err
	}
	defer util.ZeroBytes(k[:])

	hpk := sha256.Sum256(pk[:])

	var conn net.Conn
	conn, err = net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", aPort), aConnectTimeout)
	if err != nil {
		return err
	}

	rc := rpc.NewClient(&secConn{conn: conn, br: bufio.NewReader(conn), hpk: hpk, k: k})
	defer rc.Close()

	if err = op(rc); err != nil {
		return err
	}
	return nil
}

// run executes the CLI application and returns an exit code.
func run() int {

	cmd, err := processCommandLine(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n\n*** ERROR: %s\n", err.Error())
		return exitFlagParseError
	}

	if aHelp {
		cli.Usage()
		return exitHelp
	}

	log.SetPrefix("[gclpr] ")
	log.SetFlags(0)
	if !aDebug {
		log.SetOutput(io.Discard)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n\n*** ERROR: %s\n", err.Error())
		return exitNoKeys
	}

	log.Printf("Received command \"%s\" [%s]\n", cmd, aData)

	switch cmd {
	case cmdOpen:
		if aTunnel {
			var req server.TunnelOpenRequest
			var resp server.TunnelOpenResponse
			var targets []server.TunnelTarget
			var macKey []byte
			if targets, _, err = buildTunnelTargetsFromURL(aData); err != nil {
				break
			}
			log.Printf("tunnel mode selected targets=%v", targets)
			if macKey, err = generateTunnelMACKey(); err != nil {
				break
			}
			req = server.TunnelOpenRequest{
				URL:           aData,
				Targets:       targets,
				MACKey:        macKey,
				AttachTimeout: aConnectTimeout,
				IdleTimeout:   aIOTimeout,
			}
			err = doRPC(home, func(rc *rpc.Client) error {
				return rc.Call("Tunnel.Open", req, &resp)
			})
			if err == nil {
				err = startTunnelClient(resp, macKey, aConnectTimeout, nil)
			}
			break
		}
		if aOAuth {
			var req server.TunnelOpenRequest
			var resp server.TunnelOpenResponse
			var targets []server.TunnelTarget
			var macKey []byte
			log.Printf("oauth mode parsing redirect_uri timeout=%s", aIOTimeout)
			targets, err = buildTunnelTargetsFromOAuthURL(aData)
			if err != nil {
				log.Printf("oauth setup unavailable: %v; continuing with normal open", err)
				err = nil
			} else {
				log.Printf("oauth mode selected targets=%v", targets)
				if macKey, err = generateTunnelMACKey(); err != nil {
					log.Printf("oauth setup failed: %v; continuing with normal open", err)
					err = nil
				} else {
					req = server.TunnelOpenRequest{
						URL:           aData,
						Targets:       targets,
						MACKey:        macKey,
						AttachTimeout: aConnectTimeout,
						IdleTimeout:   aIOTimeout,
					}
					err = doRPC(home, func(rc *rpc.Client) error {
						return rc.Call("Tunnel.Open", req, &resp)
					})
					if err != nil {
						log.Printf("oauth setup failed: %v; continuing with normal open", err)
						err = nil
					} else {
						err = launchOAuthWorker(resp, macKey)
						if err != nil {
							log.Printf("oauth worker launch failed: %v; continuing with normal open", err)
							err = nil
						} else {
							log.Printf("oauth worker launched session=%s", resp.SessionID)
							return exitSuccess
						}
					}
				}
			}
		}
		if _, err = url.Parse(aData); err != nil {
			break
		}
		err = doRPC(home, func(rc *rpc.Client) error {
			return rc.Call("URI.Open", aData, &struct{}{})
		})
	case cmdCopy:
		err = doRPC(home, func(rc *rpc.Client) error {
			return rc.Call("Clipboard.Copy", aData, &struct{}{})
		})
	case cmdPaste:
		var resp string
		err = doRPC(home, func(rc *rpc.Client) error {
			return rc.Call("Clipboard.Paste", struct{}{}, &resp)
		})
		os.Stdout.Write([]byte(server.ConvertLE(resp, aLE)))
	case cmdGenKey:
		pk, _, er := util.ReadKeys(home)
		if er != nil {
			pk, _, err = util.CreateKeys(home)
		} else {
			err = errors.New("usable keys already exist")
		}
		if pk != nil {
			fmt.Printf("\nPublic key:\n\t%s\n", hex.EncodeToString(pk[:]))
		}
	case cmdServer:
		var pkeys map[[32]byte][32]byte
		pkeys, err = util.ReadTrustedKeys(home)
		if err == nil {
			log.Printf("Starting server with %d trusted public key(s)\n", len(pkeys))
			for k, v := range pkeys {
				log.Printf("\t%s [%s]\n", hex.EncodeToString(v[:]), hex.EncodeToString(k[:]))
			}
			// we never break this
			err = server.Serve(context.Background(), aPort, aLE, pkeys, misc.Magic(), nil, aIOTimeout)
		}
	default:
		if cmd == cmdOAuthWorker {
			err = runOAuthWorker()
			break
		}
		err = errors.New("this should never happen")
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n\n*** ERROR: %s\n", err.Error())
		return exitRPCError
	}
	return exitSuccess
}

func main() {

	cli.BoolVar(&aHelp, "help", false, "Show help")
	cli.IntVar(&aPort, "port", server.DefaultPort, "TCP port number")
	cli.StringVar(&aLE, "line-ending", "", "Convert Line Endings (LF/CRLF)")
	cli.DurationVar(&aConnectTimeout, "connect-timeout", server.DefaultConnectTimeout, "TCP connection timeout")
	cli.DurationVar(&aIOTimeout, "timeout", time.Minute, "Read/write I/O timeout")
	cli.BoolVar(&aTunnel, "tunnel", false, "Tunnel loopback http(s) targets for open")
	cli.BoolVar(&aOAuth, "oauth", false, "Tunnel OAuth redirect_uri callback listener for open")
	cli.StringVar(&aWorkerStatusAddr, "worker-status-addr", "", "Internal: oauth worker status address")
	cli.BoolVar(&aDebug, "debug", false, "Print debugging information")

	cli.Usage = func() {
		var buf strings.Builder
		cli.SetOutput(&buf)
		fmt.Fprintf(&buf, `
gclpr - copy, paste text and open browser over localhost TCP interface

Version:
    %s (%s) %s
`, misc.Version(), runtime.Version(), misc.GitHash())

		fmt.Fprintf(&buf, `
Usage:
    gclpr [options]... COMMAND [arg]

Commands:

    copy 'text'  - (client) %s
    paste        - (client) %s
    open 'url'   - (client) %s
    genkey       - (client) %s
    server       - %s

Options:

`, cmdCopy, cmdPaste, cmdOpen, cmdGenKey, cmdServer)

		cli.VisitAll(func(f *flag.Flag) {
			if strings.HasPrefix(f.Name, "worker-") {
				return
			}
			name, usage := flag.UnquoteUsage(f)
			if name != "" {
				fmt.Fprintf(&buf, "  -%s %s\n\t%s (default %q)\n", f.Name, name, usage, f.DefValue)
				return
			}
			fmt.Fprintf(&buf, "  -%s\n\t%s (default %q)\n", f.Name, f.Usage, f.DefValue)
		})
		fmt.Fprint(os.Stderr, buf.String())
	}

	os.Exit(run())
}
