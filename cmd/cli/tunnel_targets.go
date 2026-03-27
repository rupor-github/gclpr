package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/rupor-github/gclpr/server"
)

func buildTunnelTargetsFromURL(raw string) ([]server.TunnelTarget, *url.URL, error) {
	parsed, err := parseTunnelTarget(raw)
	if err != nil {
		return nil, nil, err
	}

	port, err := effectiveTunnelPort(parsed)
	if err != nil {
		return nil, nil, err
	}

	host := parsed.Hostname()
	if host == "localhost" {
		return []server.TunnelTarget{
			{ListenHost: "127.0.0.1", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))},
			{ListenHost: "::1", ListenPort: port, DialAddr: net.JoinHostPort("::1", strconv.Itoa(port))},
		}, parsed, nil
	}
	return []server.TunnelTarget{{ListenHost: host, ListenPort: port, DialAddr: net.JoinHostPort(host, strconv.Itoa(port))}}, parsed, nil
}

func effectiveTunnelPort(parsed *url.URL) (int, error) {
	if parsed.Port() != "" {
		var port int
		if _, err := fmt.Sscanf(parsed.Port(), "%d", &port); err != nil || port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid tunnel port %q", parsed.Port())
		}
		return port, nil
	}
	switch parsed.Scheme {
	case "http":
		return 80, nil
	case "https":
		return 443, nil
	default:
		return 0, fmt.Errorf("tunnel target must use http or https")
	}
}
