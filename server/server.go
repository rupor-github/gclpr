package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/nacl/sign"

	"github.com/rupor-github/gclpr/util"
)

const (
	DefaultPort           = 2850
	DefaultConnectTimeout = 10 * time.Second
	DefaultIOTimeout      = 30 * time.Second
)

type secConn struct {
	conn      net.Conn
	br        *bufio.Reader
	pkeys     map[[32]byte][32]byte
	magic     []byte
	locked    *int32
	ioTimeout time.Duration
}

func (sc *secConn) Read(p []byte) (n int, err error) {

	var hpk, pk [32]byte

	if sc.ioTimeout > 0 {
		sc.conn.SetReadDeadline(time.Now().Add(sc.ioTimeout))
	}

	in, err := util.ReadFrame(sc.br)
	if err != nil {
		return 0, err
	}

	if sc.locked != nil && atomic.LoadInt32(sc.locked) == 1 {
		log.Print("Session is locked - exiting from Read")
		return 0, io.ErrUnexpectedEOF
	}

	if len(in) <= len(sc.magic)+len(hpk)+sign.Overhead {
		log.Printf("Message is too short: %d", len(in))
		return 0, io.ErrUnexpectedEOF
	}

	// check first 6 bytes of magic - signature and major version number
	if !bytes.Equal(in[0:6], sc.magic[0:6]) {
		log.Printf("Bad signature or incompatible versions: server [%x], client [%x]", sc.magic, in[0:len(sc.magic)])
		return 0, rpc.ErrShutdown
	}

	copy(hpk[:], in[len(sc.magic):len(sc.magic)+len(hpk)])

	var ok bool
	if pk, ok = sc.pkeys[hpk]; !ok {
		log.Printf("Call with unauthorized key: %s", hex.EncodeToString(hpk[:]))
		return 0, rpc.ErrShutdown
	}

	out, ok := sign.Open([]byte{}, in[len(sc.magic)+len(hpk):], &pk)
	if !ok {
		log.Printf("Call fails verification with key: %s", hex.EncodeToString(pk[:]))
		return 0, rpc.ErrShutdown
	}
	copy(p, out)
	return len(out), nil
}

func (sc *secConn) Write(p []byte) (n int, err error) {
	if sc.ioTimeout > 0 {
		sc.conn.SetWriteDeadline(time.Now().Add(sc.ioTimeout))
	}
	if err = util.WriteFrame(sc.conn, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (sc *secConn) Close() error {
	return sc.conn.Close()
}

// Serve handles backend rpc calls.
func Serve(ctx context.Context, port int, le string, pkeys map[[32]byte][32]byte, magic []byte, locked *int32, ioTimeout time.Duration) error {

	if err := rpc.Register(NewURI()); err != nil {
		return fmt.Errorf("unable to register URI rpc object: %w", err)
	}
	if err := rpc.Register(NewClipboard(le)); err != nil {
		return fmt.Errorf("unable to register Clipboard rpc object: %w", err)
	}

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return fmt.Errorf("unable to resolve address: %w", err)
	}

	log.Printf("gclpr server listens on '%s'\n", addr)

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return fmt.Errorf("unable to listen on '%s': %w", addr, err)
	}

	// This will break the loop
	go func() {
		<-ctx.Done()
		l.Close()
	}()

	log.Print("gclpr server is ready\n")
	for {
		conn, err := l.Accept()
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				return fmt.Errorf("gclpr server is unable to accept requests: %w", err)
			}
			log.Print("gclpr server is shutting down\n")
			return nil
		}
		go func(sc *secConn) {
			defer sc.Close()
			log.Printf("gclpr server accepted request from '%s'", sc.conn.RemoteAddr())
			rpc.ServeConn(sc)
			log.Printf("gclpr server handled request from '%s'", sc.conn.RemoteAddr())
		}(&secConn{
			conn:      conn,
			br:        bufio.NewReader(conn),
			pkeys:     pkeys,
			magic:     magic,
			locked:    locked,
			ioTimeout: ioTimeout,
		})
	}
}
