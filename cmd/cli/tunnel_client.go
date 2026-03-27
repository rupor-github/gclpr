package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rupor-github/gclpr/server"
	"github.com/rupor-github/gclpr/util"
)

const (
	tunnelFrameMACSize     = sha256.Size
	maxBufferedTunnelBytes = 8 * 1024
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
	if ep.closed {
		return nil
	}
	ep.closed = true
	return ep.conn.Close()
}

func (ep *tunnelEndpoint) writeFrame(frame tunnelFrame) error {
	body := make([]byte, 1+4+len(frame.Payload))
	body[0] = byte(frame.Type)
	binary.BigEndian.PutUint32(body[1:5], frame.StreamID)
	copy(body[5:], frame.Payload)

	mac := hmac.New(sha256.New, ep.macKey)
	mac.Write(body)
	out := append(body, mac.Sum(nil)...)

	ep.mu.Lock()
	defer ep.mu.Unlock()
	return util.WriteFrame(ep.conn, out)
}

func (ep *tunnelEndpoint) readFrame() (tunnelFrame, error) {
	raw, err := util.ReadFrame(ep.br)
	if err != nil {
		return tunnelFrame{}, err
	}
	if len(raw) < 1+4+tunnelFrameMACSize {
		return tunnelFrame{}, fmt.Errorf("tunnel frame too short")
	}
	body := raw[:len(raw)-tunnelFrameMACSize]
	macBytes := raw[len(raw)-tunnelFrameMACSize:]
	mac := hmac.New(sha256.New, ep.macKey)
	mac.Write(body)
	if !hmac.Equal(macBytes, mac.Sum(nil)) {
		return tunnelFrame{}, fmt.Errorf("tunnel frame MAC mismatch")
	}
	return tunnelFrame{
		Type:     tunnelFrameType(body[0]),
		StreamID: binary.BigEndian.Uint32(body[1:5]),
		Payload:  append([]byte(nil), body[5:]...),
	}, nil
}

type tunnelLocalStream struct {
	id        uint32
	conn      net.Conn
	loggedIn  bool
	loggedOut bool
	bytesIn   int64
	bytesOut  int64
	reqBuf    []byte
	oauthDone bool
	closeOnce sync.Once
	closed    chan struct{}
}

func newTunnelLocalStream(id uint32, conn net.Conn) *tunnelLocalStream {
	return &tunnelLocalStream{id: id, conn: conn, closed: make(chan struct{})}
}

func (s *tunnelLocalStream) close() {
	s.closeOnce.Do(func() {
		s.conn.Close()
		close(s.closed)
	})
}

type tunnelClient struct {
	endpoint *tunnelEndpoint
	streams  map[uint32]*tunnelLocalStream
	mu       sync.Mutex
	closed   chan struct{}
	doneOnce sync.Once
}

func newTunnelClient(endpoint *tunnelEndpoint) *tunnelClient {
	return &tunnelClient{endpoint: endpoint, streams: make(map[uint32]*tunnelLocalStream), closed: make(chan struct{})}
}

func generateTunnelMACKey() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func startTunnelClient(resp server.TunnelOpenResponse, macKey []byte, timeout time.Duration, onAttached func() error) error {
	log.Printf("tunnel client attaching session=%s server_port=%d listeners=%v", resp.SessionID, aPort, resp.ListenAddrs)
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", aPort), timeout)
	if err != nil {
		return fmt.Errorf("unable to attach tunnel client: %w", err)
	}
	endpoint := newTunnelEndpoint(conn, macKey)
	defer endpoint.close()

	client := newTunnelClient(endpoint)
	defer client.closeAll()

	if err := endpoint.writeFrame(tunnelFrame{Type: tunnelFrameAttach, Payload: []byte(resp.SessionID)}); err != nil {
		return fmt.Errorf("unable to send tunnel attach: %w", err)
	}
	log.Printf("tunnel client attached session=%s", resp.SessionID)
	if onAttached != nil {
		if err := onAttached(); err != nil {
			return err
		}
	}
	go client.keepAlive(resp.IdleTimeout)

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.readFrames()
	}()

	select {
	case err := <-errCh:
		if err == io.EOF || err == net.ErrClosed {
			return nil
		}
		return err
	case <-time.After(resp.AttachTimeout + resp.IdleTimeout):
		return fmt.Errorf("tunnel client timed out waiting for remote closure")
	}
}

func (c *tunnelClient) readFrames() error {
	defer close(c.closed)
	for {
		frame, err := c.endpoint.readFrame()
		if err != nil {
			return err
		}
		switch frame.Type {
		case tunnelFrameOpen:
			var payload tunnelOpenPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				return fmt.Errorf("invalid tunnel open payload: %w", err)
			}
			log.Printf("tunnel client opening stream=%d dial=%s", frame.StreamID, payload.DialAddr)
			if err := c.openStream(frame.StreamID, payload.DialAddr); err != nil {
				log.Printf("tunnel client failed to open stream=%d dial=%s: %v", frame.StreamID, payload.DialAddr, err)
				_ = c.endpoint.writeFrame(tunnelFrame{Type: tunnelFrameClose, StreamID: frame.StreamID})
			}
		case tunnelFrameData:
			stream := c.getStream(frame.StreamID)
			if stream == nil {
				continue
			}
			if _, err := stream.conn.Write(frame.Payload); err != nil {
				stream.close()
				c.dropStream(frame.StreamID)
			} else if !stream.loggedIn {
				stream.loggedIn = true
				stream.bytesIn += int64(len(frame.Payload))
				if len(stream.reqBuf) < maxBufferedTunnelBytes {
					stream.reqBuf = append(stream.reqBuf, frame.Payload...)
				}
				log.Printf("tunnel client stream=%d remote->local first bytes=%q", frame.StreamID, summarizeTunnelBytes(frame.Payload))
			} else {
				stream.bytesIn += int64(len(frame.Payload))
				if len(stream.reqBuf) < maxBufferedTunnelBytes {
					stream.reqBuf = append(stream.reqBuf, frame.Payload...)
				}
			}
			if !stream.oauthDone && looksLikeOAuthSuccessRequest(stream.reqBuf) {
				stream.oauthDone = true
				log.Printf("tunnel client stream=%d detected oauth success callback request", frame.StreamID)
			}
		case tunnelFrameEOF:
			continue
		case tunnelFrameClose:
			stream := c.getStream(frame.StreamID)
			if stream != nil {
				stream.close()
				c.dropStream(frame.StreamID)
			}
		case tunnelFramePing:
			log.Printf("tunnel client received ping")
			_ = c.endpoint.writeFrame(tunnelFrame{Type: tunnelFramePong})
		case tunnelFramePong, tunnelFrameAttach, tunnelFrameError:
			log.Printf("tunnel client received frame type=%d stream=%d", frame.Type, frame.StreamID)
			continue
		default:
			return fmt.Errorf("unknown tunnel frame type %d", frame.Type)
		}
	}
}

func (c *tunnelClient) keepAlive(idleTimeout time.Duration) {
	if idleTimeout <= 0 {
		idleTimeout = 15 * time.Second
	}
	tick := idleTimeout / 2
	if tick <= 0 {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			if err := c.endpoint.writeFrame(tunnelFrame{Type: tunnelFramePing}); err != nil {
				log.Printf("tunnel client keepalive failed: %v", err)
				return
			}
			log.Printf("tunnel client sent keepalive ping")
		}
	}
}

type tunnelOpenPayload struct {
	DialAddr string `json:"dial_addr"`
}

func (c *tunnelClient) openStream(id uint32, dialAddr string) error {
	log.Printf("tunnel client dialing stream=%d addr=%s", id, dialAddr)
	conn, err := net.DialTimeout("tcp", dialAddr, aConnectTimeout)
	if err != nil {
		return err
	}
	stream := newTunnelLocalStream(id, conn)
	c.mu.Lock()
	c.streams[id] = stream
	c.mu.Unlock()
	go c.pipeLocalToRemote(stream)
	log.Printf("tunnel client connected stream=%d addr=%s", id, dialAddr)
	return nil
}

func (c *tunnelClient) pipeLocalToRemote(stream *tunnelLocalStream) {
	defer c.dropStream(stream.id)
	buf := make([]byte, 32*1024)
	for {
		n, err := stream.conn.Read(buf)
		if n > 0 {
			if !stream.loggedOut {
				stream.loggedOut = true
				stream.bytesOut += int64(n)
				log.Printf("tunnel client stream=%d local->remote first bytes=%q", stream.id, summarizeTunnelBytes(buf[:n]))
			} else {
				stream.bytesOut += int64(n)
			}
			if writeErr := c.endpoint.writeFrame(tunnelFrame{Type: tunnelFrameData, StreamID: stream.id, Payload: append([]byte(nil), buf[:n]...)}); writeErr != nil {
				stream.close()
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				log.Printf("tunnel client stream=%d local EOF bytes_in=%d bytes_out=%d", stream.id, stream.bytesIn, stream.bytesOut)
				log.Printf("tunnel client stream=%d local closed write side", stream.id)
				_ = c.endpoint.writeFrame(tunnelFrame{Type: tunnelFrameEOF, StreamID: stream.id})
				if stream.oauthDone && stream.bytesOut > 0 {
					log.Printf("tunnel client stream=%d completed oauth callback response; closing worker tunnel", stream.id)
					c.finish()
				}
				return
			}
			log.Printf("tunnel client stream=%d local socket terminated bytes_in=%d bytes_out=%d", stream.id, stream.bytesIn, stream.bytesOut)
			log.Printf("tunnel client stream=%d local read failed: %v", stream.id, err)
			_ = c.endpoint.writeFrame(tunnelFrame{Type: tunnelFrameClose, StreamID: stream.id})
			return
		}
	}
}

func (c *tunnelClient) getStream(id uint32) *tunnelLocalStream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[id]
}

func (c *tunnelClient) dropStream(id uint32) {
	c.mu.Lock()
	stream, ok := c.streams[id]
	if ok {
		delete(c.streams, id)
	}
	c.mu.Unlock()
	if ok {
		log.Printf("tunnel client dropping stream=%d", id)
		stream.close()
	}
}

func (c *tunnelClient) finish() {
	c.doneOnce.Do(func() {
		_ = c.endpoint.close()
	})
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

func looksLikeOAuthSuccessRequest(data []byte) bool {
	text := string(data)
	if !strings.Contains(text, "\r\n\r\n") {
		return false
	}
	return strings.HasPrefix(text, "GET /") && strings.Contains(text, "code=") && strings.Contains(text, "state=")
}

func (c *tunnelClient) closeAll() {
	c.mu.Lock()
	streams := make([]*tunnelLocalStream, 0, len(c.streams))
	for _, stream := range c.streams {
		streams = append(streams, stream)
	}
	c.streams = make(map[uint32]*tunnelLocalStream)
	c.mu.Unlock()
	for _, stream := range streams {
		stream.close()
	}
}
