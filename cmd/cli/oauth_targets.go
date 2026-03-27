package main

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"

	"github.com/rupor-github/gclpr/server"
)

func buildTunnelTargetsFromOAuthURL(raw string) ([]server.TunnelTarget, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth URL: %w", err)
	}
	redirectValue := parsed.Query().Get("redirect_uri")
	if redirectValue == "" {
		return nil, fmt.Errorf("oauth URL is missing redirect_uri")
	}
	redirectURL, err := server.ParseTunnelURL(redirectValue)
	if err != nil {
		return nil, fmt.Errorf("oauth redirect_uri is invalid: %w", err)
	}
	port, err := effectiveTunnelPort(redirectURL)
	if err != nil {
		return nil, err
	}
	host := redirectURL.Hostname()
	log.Printf("oauth redirect_uri parsed raw=%q host=%q port=%d", redirectValue, host, port)
	if host == "localhost" {
		return []server.TunnelTarget{{ListenHost: "localhost", ListenPort: port, DialAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}}, nil
	}
	return []server.TunnelTarget{{ListenHost: host, ListenPort: port, DialAddr: net.JoinHostPort(host, strconv.Itoa(port))}}, nil
}
