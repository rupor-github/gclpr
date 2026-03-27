package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/rpc"
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
// It uses rpc.DefaultServer internally, so it can only be called once per process.
func Serve(ctx context.Context, port int, le string, pkeys map[[32]byte][32]byte, magic []byte, locked *int32, ioTimeout time.Duration) error {
	tunnel := NewTunnel()

	if err := rpc.Register(NewURI()); err != nil {
		return fmt.Errorf("unable to register URI rpc object: %w", err)
	}
	if err := rpc.Register(NewClipboard(le)); err != nil {
		return fmt.Errorf("unable to register Clipboard rpc object: %w", err)
	}
	if err := rpc.Register(tunnel); err != nil {
		return fmt.Errorf("unable to register Tunnel rpc object: %w", err)
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
			if !errors.Is(err, net.ErrClosed) {
				return fmt.Errorf("gclpr server is unable to accept requests: %w", err)
			}
			log.Print("gclpr server is shutting down\n")
			return nil
		}
		go func(conn net.Conn) {
			handled, rpcReader := tunnel.attach(conn)
			if handled {
				return
			}
			sc := &secConn{
				conn:      conn,
				br:        rpcReader,
				pkeys:     pkeys,
				magic:     magic,
				locked:    locked,
				ioTimeout: ioTimeout,
			}
			defer sc.Close()
			log.Printf("gclpr server accepted request from '%s'", sc.conn.RemoteAddr())
			rpc.ServeConn(sc)
			log.Printf("gclpr server handled request from '%s'", sc.conn.RemoteAddr())
		}(conn)
	}
}

func (t *Tunnel) attach(conn net.Conn) (bool, *bufio.Reader) {
	br := bufio.NewReader(conn)
	prefix, err := br.Peek(4)
	if err != nil {
		conn.Close()
		return true, nil
	}
	if len(prefix) != 4 {
		conn.Close()
		return true, nil
	}
	frameLen := int(uint32(prefix[0])<<24 | uint32(prefix[1])<<16 | uint32(prefix[2])<<8 | uint32(prefix[3]))
	if frameLen <= 0 || frameLen > util.MaxFrameSize {
		return false, br
	}
	raw, err := util.ReadFrame(br)
	if err != nil {
		conn.Close()
		return true, nil
	}
	replay := bufio.NewReader(io.MultiReader(bytes.NewReader(appendFrame(raw)), br))
	if len(raw) < 1+4+tunnelMACSize {
		return false, replay
	}
	frameType := tunnelFrameType(raw[0])
	if frameType != tunnelFrameAttach {
		return false, replay
	}
	sessionIDBytes := raw[5 : len(raw)-tunnelMACSize]
	if len(sessionIDBytes) == 0 {
		conn.Close()
		return true, nil
	}
	sessionID := string(sessionIDBytes)
	t.mu.Lock()
	session, ok := t.sessions[sessionID]
	t.mu.Unlock()
	if !ok {
		conn.Close()
		return true, nil
	}
	endpoint := &tunnelEndpoint{conn: conn, br: replay, macKey: append([]byte(nil), session.macKey...)}
	frame, err := endpoint.readFrame()
	if err != nil || frame.Type != tunnelFrameAttach || string(frame.Payload) != sessionID {
		endpoint.close()
		session.closeReason = "attach validation failed"
		t.closeSession(sessionID)
		return true, nil
	}
	if session.attachTimer != nil {
		session.attachTimer.Stop()
	}
	if session.peer != nil {
		endpoint.close()
		return true, nil
	}
	session.peer = endpoint
	session.markPeerReady()
	session.touch()
	session.launchOnce.Do(func() {
		if err := opener(session.openURL); err != nil {
			log.Printf("unable to open tunneled URI %q: %v", session.openURL, err)
			session.closeReason = fmt.Sprintf("browser open failed: %v", err)
			t.closeSession(sessionID)
		}
	})
	go t.readPeerFrames(session)
	return true, nil
}

func appendFrame(payload []byte) []byte {
	buf := make([]byte, 4+len(payload))
	buf[0] = byte(len(payload) >> 24)
	buf[1] = byte(len(payload) >> 16)
	buf[2] = byte(len(payload) >> 8)
	buf[3] = byte(len(payload))
	copy(buf[4:], payload)
	return buf
}
