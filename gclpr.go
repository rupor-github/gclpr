package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/rpc"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"

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
	default:
		return fmt.Sprintf("bad command %d", c)
	}
}

var (
	aPort  int
	aLE    string
	aHelp  bool
	aDebug bool
	aData  string
	cli    = flag.NewFlagSet("gclpr", flag.ContinueOnError)
)

func getCommand(args []string) (cmd command, aliased bool, err error) {

	aliased = true

	switch {
	case regexp.MustCompile(`/?xdg-open$`).MatchString(args[0]):
		cmd = cmdOpen
		return
	case regexp.MustCompile(`/?pbpaste$`).MatchString(args[0]):
		cmd = cmdPaste
		return
	case regexp.MustCompile(`/?pbcopy$`).MatchString(args[0]):
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

	if cmd == cmdPaste || cmd == cmdServer || cmd == cmdGenKey {
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

	if len(arg) != 0 {
		aData = arg
	} else {
		var b []byte
		if b, err = ioutil.ReadAll(os.Stdin); err != nil {
			return
		}
		aData = string(b)
	}
	return
}

type secConn struct {
	conn net.Conn
	pk   *[32]byte
	k    *[64]byte
}

func (sc *secConn) Read(p []byte) (n int, err error) {
	return sc.conn.Read(p)
}

func (sc *secConn) Write(p []byte) (n int, err error) {
	out := sign.Sign(sc.pk[:], p, sc.k)
	n, err = sc.conn.Write(out)
	if err == nil {
		n = len(p)
	}
	return
}

func (sc *secConn) Close() error {
	return sc.conn.Close()
}

func doRpc(home string, op func(*rpc.Client) error) error {

	pk, k, err := util.ReadKeys(home)
	if err != nil {
		return err
	}

	var conn net.Conn
	conn, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", aPort))
	if err != nil {
		return err
	}

	rc := rpc.NewClient(&secConn{conn: conn, pk: pk, k: k})
	defer rc.Close()

	if err = op(rc); err != nil {
		return err
	}
	return nil
}

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
		log.SetOutput(ioutil.Discard)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n\n*** ERROR: %s\n", err.Error())
		return exitNoKeys
	}

	log.Printf("Received command \"%s\" [%s]\n", cmd, aData)

	switch cmd {
	case cmdOpen:
		if _, err = url.ParseRequestURI(aData); err != nil {
			break
		}
		err = doRpc(home, func(rc *rpc.Client) error {
			return rc.Call("URI.Open", aData, &struct{}{})
		})
	case cmdCopy:
		err = doRpc(home, func(rc *rpc.Client) error {
			return rc.Call("Clipboard.Copy", aData, &struct{}{})
		})
	case cmdPaste:
		var resp string
		err = doRpc(home, func(rc *rpc.Client) error {
			return rc.Call("Clipboard.Paste", struct{}{}, &resp)
		})
		os.Stdout.Write([]byte(server.ConvertLE(resp, aLE)))
	case cmdServer:
		var pkeys map[[32]byte]struct{}
		pkeys, err = util.ReadTrustedKeys(home)
		if err == nil {
			log.Printf("Starting server with %d trusted public key(s)\n", len(pkeys))
			for k := range pkeys {
				log.Printf("\t%s\n", hex.EncodeToString(k[:]))
			}
			err = server.Serve(aPort, aLE, pkeys)
		}
	case cmdGenKey:
		pk, _, er := util.ReadKeys(home)
		if er != nil {
			pk, _, err = util.CreateKeys(home)
		} else {
			err = errors.New("usable keys already exit")
		}
		if pk != nil {
			fmt.Printf("\nPublic key:\n\t%s\n", hex.EncodeToString(pk[:]))
		}
	default:
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
	cli.BoolVar(&aDebug, "debug", false, "Print debugging information")

	cli.Usage = func() {
		var buf strings.Builder
		cli.SetOutput(&buf)
		fmt.Fprintf(&buf, `
gclpr - copy, paste and open browser over localhost TCP interface

Version:
    %s (%s) %s
`, misc.GetVersion(), runtime.Version(), misc.GetGitHash())

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

		cli.PrintDefaults()
		fmt.Fprint(os.Stderr, buf.String())
	}

	os.Exit(run())
}
