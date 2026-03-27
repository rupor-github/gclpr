package server

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/rupor-github/gclpr/util"
)

const (
	tunnelMACKeySize = 32
	tunnelMACSize    = sha256.Size
)

type tunnelFrameType byte

const (
	tunnelFrameAttach tunnelFrameType = iota + 1
	tunnelFrameOpen
	tunnelFrameData
	tunnelFrameEOF
	tunnelFrameClose
	tunnelFramePing
	tunnelFramePong
	tunnelFrameError
)

type tunnelFrame struct {
	Type     tunnelFrameType
	StreamID uint32
	Payload  []byte
}

type tunnelOpenPayload struct {
	DialAddr string `json:"dial_addr"`
}

type tunnelEndpoint struct {
	conn   net.Conn
	br     *bufio.Reader
	macKey []byte
	mu     sync.Mutex
	closed bool
}

func newTunnelEndpoint(conn net.Conn, macKey []byte) *tunnelEndpoint {
	return &tunnelEndpoint{conn: conn, br: bufio.NewReader(conn), macKey: append([]byte(nil), macKey...)}
}

func (ep *tunnelEndpoint) close() error {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	if !ep.closed {
		ep.closed = true
		return ep.conn.Close()
	}
	return nil
}

func (ep *tunnelEndpoint) writeFrame(frame tunnelFrame) error {
	bodyLen := 1 + 4 + len(frame.Payload)
	body := make([]byte, bodyLen)
	body[0] = byte(frame.Type)
	binary.BigEndian.PutUint32(body[1:5], frame.StreamID)
	copy(body[5:], frame.Payload)

	mac := hmac.New(sha256.New, ep.macKey)
	mac.Write(body)
	out := append(body, mac.Sum(nil)...)

	ep.mu.Lock()
	defer ep.mu.Unlock()
	if err := util.WriteFrame(ep.conn, out); err != nil {
		return err
	}
	return nil
}

func (ep *tunnelEndpoint) readFrame() (tunnelFrame, error) {
	raw, err := util.ReadFrame(ep.br)
	if err != nil {
		return tunnelFrame{}, err
	}
	if len(raw) < 1+4+tunnelMACSize {
		return tunnelFrame{}, fmt.Errorf("tunnel frame too short")
	}
	body := raw[:len(raw)-tunnelMACSize]
	wantMAC := raw[len(raw)-tunnelMACSize:]
	mac := hmac.New(sha256.New, ep.macKey)
	mac.Write(body)
	if !hmac.Equal(wantMAC, mac.Sum(nil)) {
		return tunnelFrame{}, fmt.Errorf("tunnel frame MAC mismatch")
	}
	return tunnelFrame{
		Type:     tunnelFrameType(body[0]),
		StreamID: binary.BigEndian.Uint32(body[1:5]),
		Payload:  append([]byte(nil), body[5:]...),
	}, nil
}

type tunnelStream struct {
	id        uint32
	session   *tunnelSession
	browser   net.Conn
	loggedIn  bool
	loggedOut bool
	bytesIn   int64
	bytesOut  int64
	closeOnce sync.Once
	closed    chan struct{}
}

func newTunnelStream(session *tunnelSession, id uint32, browser net.Conn) *tunnelStream {
	return &tunnelStream{id: id, session: session, browser: browser, closed: make(chan struct{})}
}

func (s *tunnelStream) close() {
	s.closeOnce.Do(func() {
		s.browser.Close()
		close(s.closed)
	})
}

func (s *tunnelStream) closeWrite() {
	if tcpConn, ok := s.browser.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
		return
	}
	s.close()
}

func (t *Tunnel) serveBrowserListener(session *tunnelSession, listener *tunnelListener) {
	for {
		conn, err := listener.listener.AcceptTCP()
		if err != nil {
			select {
			case <-session.closed:
				log.Printf("tunnel session %s listener %s closed", session.id, listener.addr)
				return
			default:
			}
			log.Printf("tunnel session %s listener %s accept failed: %v", session.id, listener.addr, err)
			return
		}
		log.Printf("tunnel session %s accepted browser connection from %s on %s -> %s", session.id, conn.RemoteAddr(), listener.addr, listener.dialAddr)

		select {
		case <-session.peerReady:
		case <-session.closed:
			log.Printf("tunnel session %s closed before stream setup for browser %s", session.id, conn.RemoteAddr())
			conn.Close()
			return
		}

		streamID := session.nextID()
		stream := newTunnelStream(session, streamID, conn)
		session.streamMu.Lock()
		session.streams[streamID] = stream
		session.streamMu.Unlock()
		session.touch()

		payload, err := json.Marshal(tunnelOpenPayload{DialAddr: listener.dialAddr})
		if err != nil {
			log.Printf("tunnel session %s failed to encode open payload for stream %d: %v", session.id, streamID, err)
			stream.close()
			t.closeSession(session.id)
			return
		}
		if err := session.peer.writeFrame(tunnelFrame{Type: tunnelFrameOpen, StreamID: streamID, Payload: payload}); err != nil {
			log.Printf("tunnel session %s failed to announce stream %d to peer: %v", session.id, streamID, err)
			stream.close()
			t.closeSession(session.id)
			return
		}
		log.Printf("tunnel session %s opened stream %d browser=%s dial=%s", session.id, streamID, conn.RemoteAddr(), listener.dialAddr)

		go t.pipeBrowserToPeer(session, stream)
	}
}

func (t *Tunnel) pipeBrowserToPeer(session *tunnelSession, stream *tunnelStream) {
	defer t.dropStream(session, stream.id)
	buf := make([]byte, 32*1024)
	for {
		n, err := stream.browser.Read(buf)
		if n > 0 {
			if !stream.loggedOut {
				stream.loggedOut = true
				log.Printf("tunnel session %s stream %d browser->client first bytes=%q", session.id, stream.id, summarizeTunnelBytes(buf[:n]))
			}
			stream.bytesOut += int64(n)
			log.Printf("tunnel session %s stream %d browser->client bytes=%d total_out=%d", session.id, stream.id, n, stream.bytesOut)
			session.touch()
			if writeErr := session.peer.writeFrame(tunnelFrame{Type: tunnelFrameData, StreamID: stream.id, Payload: append([]byte(nil), buf[:n]...)}); writeErr != nil {
				log.Printf("tunnel session %s stream %d browser->client forward failed total_out=%d: %v", session.id, stream.id, stream.bytesOut, writeErr)
				stream.close()
				t.closeSession(session.id)
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				log.Printf("tunnel session %s stream %d browser closed write side total_out=%d total_in=%d", session.id, stream.id, stream.bytesOut, stream.bytesIn)
				_ = session.peer.writeFrame(tunnelFrame{Type: tunnelFrameEOF, StreamID: stream.id})
				return
			}
			log.Printf("tunnel session %s stream %d browser read failed total_out=%d total_in=%d: %v", session.id, stream.id, stream.bytesOut, stream.bytesIn, err)
			_ = session.peer.writeFrame(tunnelFrame{Type: tunnelFrameClose, StreamID: stream.id})
			return
		}
	}
}

func (t *Tunnel) readPeerFrames(session *tunnelSession) {
	for {
		frame, err := session.peer.readFrame()
		if err != nil {
			log.Printf("tunnel session %s peer read failed: %v", session.id, err)
			session.closeReason = fmt.Sprintf("peer read failed: %v", err)
			t.closeSession(session.id)
			return
		}
		switch frame.Type {
		case tunnelFrameAttach:
			log.Printf("tunnel session %s received duplicate attach frame", session.id)
			continue
		case tunnelFrameData:
			session.touch()
			stream := t.getStream(session, frame.StreamID)
			if stream == nil {
				log.Printf("tunnel session %s dropping data for unknown stream %d", session.id, frame.StreamID)
				continue
			}
			if _, err := stream.browser.Write(frame.Payload); err != nil {
				log.Printf("tunnel session %s stream %d client->browser write failed total_in=%d total_out=%d: %v", session.id, frame.StreamID, stream.bytesIn, stream.bytesOut, err)
				stream.close()
				t.dropStream(session, frame.StreamID)
			} else if !stream.loggedIn {
				stream.loggedIn = true
				stream.bytesIn += int64(len(frame.Payload))
				log.Printf("tunnel session %s stream %d client->browser first bytes=%q", session.id, frame.StreamID, summarizeTunnelBytes(frame.Payload))
			} else {
				stream.bytesIn += int64(len(frame.Payload))
			}
			log.Printf("tunnel session %s stream %d client->browser bytes=%d total_in=%d", session.id, frame.StreamID, len(frame.Payload), stream.bytesIn)
		case tunnelFrameEOF:
			session.touch()
			stream := t.getStream(session, frame.StreamID)
			if stream != nil {
				log.Printf("tunnel session %s stream %d peer closed write side total_in=%d total_out=%d", session.id, frame.StreamID, stream.bytesIn, stream.bytesOut)
				stream.closeWrite()
			}
		case tunnelFrameClose:
			session.touch()
			stream := t.getStream(session, frame.StreamID)
			if stream != nil {
				log.Printf("tunnel session %s stream %d peer requested close total_in=%d total_out=%d", session.id, frame.StreamID, stream.bytesIn, stream.bytesOut)
				stream.close()
				t.dropStream(session, frame.StreamID)
			}
		case tunnelFramePing:
			log.Printf("tunnel session %s received ping", session.id)
			_ = session.peer.writeFrame(tunnelFrame{Type: tunnelFramePong})
		case tunnelFramePong, tunnelFrameError:
			log.Printf("tunnel session %s received frame type %d", session.id, frame.Type)
			continue
		default:
			log.Printf("tunnel session %s received unknown frame type %d", session.id, frame.Type)
			session.closeReason = fmt.Sprintf("unknown frame type %d", frame.Type)
			t.closeSession(session.id)
			return
		}
	}
}

func summarizeTunnelBytes(data []byte) string {
	const limit = 120
	text := strings.ReplaceAll(string(data), "\r", "\\r")
	text = strings.ReplaceAll(text, "\n", "\\n")
	if len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}

func (t *Tunnel) getStream(session *tunnelSession, id uint32) *tunnelStream {
	session.streamMu.Lock()
	defer session.streamMu.Unlock()
	return session.streams[id]
}

func (t *Tunnel) dropStream(session *tunnelSession, id uint32) {
	session.streamMu.Lock()
	stream, ok := session.streams[id]
	if ok {
		delete(session.streams, id)
	}
	session.streamMu.Unlock()
	if ok {
		log.Printf("tunnel session %s dropping stream %d", session.id, id)
		stream.close()
	}
}
