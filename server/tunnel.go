package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	DefaultTunnelAttachTimeout = DefaultConnectTimeout
	DefaultTunnelIdleTimeout   = DefaultIOTimeout
)

// TunnelOpenRequest reserves server-side loopback listener(s) for a browser tunnel.
type TunnelOpenRequest struct {
	URL           string
	Targets       []TunnelTarget
	MACKey        []byte
	AttachTimeout time.Duration
	IdleTimeout   time.Duration
}

// TunnelTarget describes one server-side listener and its matching client-side dial target.
type TunnelTarget struct {
	ListenHost string
	ListenPort int
	DialAddr   string
}

// TunnelOpenResponse describes the reserved tunnel session.
type TunnelOpenResponse struct {
	SessionID     string
	OpenURL       string
	ListenPort    int
	ListenAddrs   []string
	AttachTimeout time.Duration
	IdleTimeout   time.Duration
}

type tunnelSession struct {
	id           string
	url          *url.URL
	openURL      string
	listenAddrs  []string
	listeners    []*tunnelListener
	attachTimer  *time.Timer
	idleTimer    *time.Timer
	closeReason  string
	attachTO     time.Duration
	idleTO       time.Duration
	createdAt    time.Time
	macKey       []byte
	peerReady    chan struct{}
	peerOnce     sync.Once
	launchOnce   sync.Once
	timerMu      sync.Mutex
	peer         *tunnelEndpoint
	streamMu     sync.Mutex
	streams      map[uint32]*tunnelStream
	nextStreamID uint32
	closeOnce    sync.Once
	closed       chan struct{}
}

type tunnelListener struct {
	listener *net.TCPListener
	addr     string
	dialAddr string
	fallback bool
}

func (s *tunnelSession) close() {
	s.closeOnce.Do(func() {
		s.timerMu.Lock()
		if s.attachTimer != nil {
			s.attachTimer.Stop()
		}
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		s.timerMu.Unlock()
		for _, listener := range s.listeners {
			listener.listener.Close()
		}
		if s.peer != nil {
			s.peer.close()
		}
		s.streamMu.Lock()
		for _, stream := range s.streams {
			stream.close()
		}
		s.streamMu.Unlock()
		close(s.closed)
	})
}

func (s *tunnelSession) markPeerReady() {
	s.peerOnce.Do(func() {
		close(s.peerReady)
	})
}

func (s *tunnelSession) startIdleTimer(onIdle func()) {
	if s.idleTO <= 0 {
		return
	}
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.idleTO, onIdle)
}

func (s *tunnelSession) touch() {
	if s.idleTO <= 0 {
		return
	}
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Reset(s.idleTO)
	}
}

// Tunnel handles tunnel session negotiation.
type Tunnel struct {
	mu           sync.Mutex
	sessions     map[string]*tunnelSession
	listenTCP    func(network string, laddr *net.TCPAddr) (*net.TCPListener, error)
	newSessionID func() (string, error)
	now          func() time.Time
}

// NewTunnel initializes Tunnel structure.
func NewTunnel() *Tunnel {
	return &Tunnel{
		sessions:     make(map[string]*tunnelSession),
		listenTCP:    net.ListenTCP,
		newSessionID: randomTunnelSessionID,
		now:          time.Now,
	}
}

// Open reserves server-side listener(s) for a future browser tunnel connection.
func (t *Tunnel) Open(req TunnelOpenRequest, resp *TunnelOpenResponse) error {
	parsed, err := ParseOpenURI(req.URL)
	if err != nil {
		return err
	}
	if len(req.Targets) == 0 {
		return fmt.Errorf("tunnel targets are required")
	}

	attachTimeout := req.AttachTimeout
	if attachTimeout <= 0 {
		attachTimeout = DefaultTunnelAttachTimeout
	}
	idleTimeout := req.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultTunnelIdleTimeout
	}

	sessionID, err := t.newSessionID()
	if err != nil {
		return fmt.Errorf("unable to create tunnel session id: %w", err)
	}

	targets, listenAddrs, err := t.bindListeners(req.Targets)
	if err != nil {
		return err
	}
	log.Printf("tunnel session request url=%q targets=%d attach_timeout=%s idle_timeout=%s", parsed.String(), len(req.Targets), attachTimeout, idleTimeout)
	if len(req.MACKey) != tunnelMACKeySize {
		for _, listener := range targets {
			listener.listener.Close()
		}
		return fmt.Errorf("tunnel MAC key must be %d bytes", tunnelMACKeySize)
	}

	session := &tunnelSession{
		id:          sessionID,
		url:         parsed,
		openURL:     rewriteTunnelOpenURL(parsed, targets),
		listenAddrs: listenAddrs,
		listeners:   targets,
		attachTO:    attachTimeout,
		idleTO:      idleTimeout,
		createdAt:   t.now(),
		macKey:      append([]byte(nil), req.MACKey...),
		peerReady:   make(chan struct{}),
		streams:     make(map[uint32]*tunnelStream),
		closed:      make(chan struct{}),
	}
	session.attachTimer = time.AfterFunc(attachTimeout, func() {
		session.closeReason = fmt.Sprintf("attach timeout after %s", attachTimeout)
		t.closeSession(sessionID)
	})
	session.startIdleTimer(func() {
		session.closeReason = fmt.Sprintf("idle timeout after %s", idleTimeout)
		t.closeSession(sessionID)
	})

	t.mu.Lock()
	t.sessions[sessionID] = session
	t.mu.Unlock()

	resp.SessionID = sessionID
	resp.OpenURL = session.openURL
	if len(session.listeners) > 0 {
		resp.ListenPort = session.listeners[0].listener.Addr().(*net.TCPAddr).Port
	}
	resp.ListenAddrs = append([]string(nil), session.listenAddrs...)
	resp.AttachTimeout = session.attachTO
	resp.IdleTimeout = session.idleTO
	log.Printf("tunnel session %s reserved listeners=%v", sessionID, session.listenAddrs)

	for _, listener := range session.listeners {
		go t.serveBrowserListener(session, listener)
	}

	return nil
}

func (t *Tunnel) closeSession(id string) {
	t.mu.Lock()
	session, ok := t.sessions[id]
	if ok {
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	if ok {
		reason := session.closeReason
		if reason == "" {
			reason = "explicit close"
		}
		log.Printf("closing tunnel session %s listeners=%d streams=%d reason=%s", id, len(session.listeners), len(session.streams), reason)
		session.close()
	}
}

func ParseTunnelURL(raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid tunnel target: %w", err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("tunnel target must be an absolute URL")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("tunnel target must use http or https")
	}
	if !isLoopbackTunnelHost(parsed.Hostname()) {
		return nil, fmt.Errorf("tunnel target must use localhost or a loopback address")
	}

	return parsed, nil
}

func isLoopbackTunnelHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func tunnelEffectivePort(parsed *url.URL) (int, error) {
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("tunnel target port %q is invalid", port)
		}
		return n, nil
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http":
		return 80, nil
	case "https":
		return 443, nil
	default:
		return 0, fmt.Errorf("tunnel target must use http or https")
	}
}

func (t *Tunnel) bindListeners(targets []TunnelTarget) ([]*tunnelListener, []string, error) {
	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("tunnel targets are required")
	}

	listeners := make([]*tunnelListener, 0, len(targets))
	listenAddrs := make([]string, 0, len(targets))
	bindErrs := make([]error, 0, len(targets))

	for _, target := range targets {
		if err := validateTunnelTarget(target); err != nil {
			log.Printf("rejecting tunnel target host=%q port=%d dial=%q: %v", target.ListenHost, target.ListenPort, target.DialAddr, err)
			bindErrs = append(bindErrs, err)
			continue
		}
		log.Printf("binding tunnel target host=%q port=%d dial=%q", target.ListenHost, target.ListenPort, target.DialAddr)
		addrs, err := tunnelListenTargets(target.ListenHost, target.ListenPort)
		if err != nil {
			bindErrs = append(bindErrs, err)
			continue
		}
		for _, addr := range addrs {
			listener, fallback, err := t.listenTunnelAddr(addr)
			if err != nil {
				log.Printf("failed binding tunnel listener %s for dial=%q: %v", addr.String(), target.DialAddr, err)
				bindErrs = append(bindErrs, fmt.Errorf("%s: %w", addr.String(), err))
				continue
			}
			targetListener := &tunnelListener{listener: listener, addr: listener.Addr().String(), dialAddr: target.DialAddr, fallback: fallback}
			listeners = append(listeners, targetListener)
			listenAddrs = append(listenAddrs, targetListener.addr)
			log.Printf("bound tunnel listener %s -> %s", targetListener.addr, targetListener.dialAddr)
		}
	}

	if len(listeners) == 0 {
		return nil, nil, fmt.Errorf("unable to reserve tunnel listeners: %w", errors.Join(bindErrs...))
	}

	return listeners, listenAddrs, nil
}

func (t *Tunnel) listenTunnelAddr(addr *net.TCPAddr) (*net.TCPListener, bool, error) {
	listener, err := t.listenTCP("tcp", addr)
	if err == nil {
		return listener, false, nil
	}
	if addr.Port == 0 || !errors.Is(err, syscall.EADDRINUSE) {
		return nil, false, err
	}
	fallbackAddr := &net.TCPAddr{IP: append(net.IP(nil), addr.IP...), Port: 0}
	listener, fallbackErr := t.listenTCP("tcp", fallbackAddr)
	if fallbackErr != nil {
		return nil, false, errors.Join(err, fallbackErr)
	}
	log.Printf("tunnel listener %s unavailable, using random port %s", addr.String(), listener.Addr())
	return listener, true, nil
}

func rewriteTunnelOpenURL(parsed *url.URL, listeners []*tunnelListener) string {
	if !tunnelListenersUseFallback(listeners) || len(listeners) == 0 {
		return parsed.String()
	}
	actualAddr := listeners[0].listener.Addr().(*net.TCPAddr)
	actualHostPort := net.JoinHostPort(actualAddr.IP.String(), strconv.Itoa(actualAddr.Port))

	if isLoopbackTunnelURL(parsed) {
		updated := *parsed
		updated.Host = actualHostPort
		return updated.String()
	}

	redirectValue := parsed.Query().Get("redirect_uri")
	if redirectValue == "" {
		return parsed.String()
	}
	redirectURL, err := ParseTunnelURL(redirectValue)
	if err != nil {
		return parsed.String()
	}
	redirectURL.Host = actualHostPort
	updated := *parsed
	query := updated.Query()
	query.Set("redirect_uri", redirectURL.String())
	updated.RawQuery = query.Encode()
	return updated.String()
}

func tunnelListenersUseFallback(listeners []*tunnelListener) bool {
	for _, listener := range listeners {
		if listener.fallback {
			return true
		}
	}
	return false
}

func isLoopbackTunnelURL(parsed *url.URL) bool {
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	return isLoopbackTunnelHost(parsed.Hostname())
}

func validateTunnelTarget(target TunnelTarget) error {
	if target.ListenPort < 1 || target.ListenPort > 65535 {
		return fmt.Errorf("tunnel target port %d is invalid", target.ListenPort)
	}
	if _, _, err := net.SplitHostPort(target.DialAddr); err != nil {
		return fmt.Errorf("tunnel dial address %q is invalid: %w", target.DialAddr, err)
	}
	if !isLoopbackTunnelHost(target.ListenHost) {
		return fmt.Errorf("tunnel target must use localhost or a loopback address")
	}
	return nil
}

func tunnelListenTargets(host string, port int) ([]*net.TCPAddr, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return []*net.TCPAddr{{IP: net.ParseIP("127.0.0.1"), Port: port}, {IP: net.ParseIP("::1"), Port: port}}, nil
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("tunnel target must use localhost or a loopback address")
	}
	return []*net.TCPAddr{{IP: ip, Port: port}}, nil
}

func randomTunnelSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *tunnelSession) nextID() uint32 {
	return atomic.AddUint32(&s.nextStreamID, 1)
}
