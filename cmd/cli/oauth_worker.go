package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rupor-github/gclpr/server"
)

var applyWorkerDetach = func(cmd *exec.Cmd) {}

func launchOAuthWorker(resp server.TunnelOpenResponse, macKey []byte) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	statusLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer statusLn.Close()

	statusAddr := statusLn.Addr().String()
	var logFile *os.File
	if aDebug {
		logFile, err = os.CreateTemp("", "gclpr-worker-*.log")
		if err != nil {
			return err
		}
		log.Printf("oauth worker log file: %s", logFile.Name())
	}

	cmd := exec.Command(executable, "internal-oauth-worker",
		"--port", fmt.Sprintf("%d", aPort),
		"--connect-timeout", aConnectTimeout.String(),
		"--timeout", aIOTimeout.String(),
		"--worker-session-id", resp.SessionID,
		"--worker-mac-key", hex.EncodeToString(macKey),
		"--worker-status-addr", statusAddr,
	)
	if aDebug {
		cmd.Args = append(cmd.Args, "--debug")
	}
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	applyWorkerDetach(cmd)
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return err
	}
	if logFile != nil {
		logFile.Close()
	}

	resultCh := make(chan error, 1)
	go func() {
		conn, err := statusLn.Accept()
		if err != nil {
			resultCh <- err
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil && err != io.EOF {
			resultCh <- err
			return
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "OK":
			resultCh <- nil
		case strings.HasPrefix(line, "ERR "):
			resultCh <- errors.New(strings.TrimPrefix(line, "ERR "))
		default:
			resultCh <- fmt.Errorf("oauth worker failed to report readiness")
		}
	}()

	select {
	case err := <-resultCh:
		_ = cmd.Process.Release()
		return err
	case <-time.After(aConnectTimeout):
		_ = cmd.Process.Release()
		return fmt.Errorf("oauth worker startup timed out")
	}
}

func runOAuthWorker() error {
	report := func(msg string) {}
	if aWorkerStatusAddr != "" {
		conn, err := net.DialTimeout("tcp", aWorkerStatusAddr, aConnectTimeout)
		if err == nil {
			defer conn.Close()
			report = func(msg string) {
				_, _ = io.WriteString(conn, msg+"\n")
			}
		}
	}
	if aWorkerSessionID == "" {
		err := fmt.Errorf("oauth worker session id is required")
		report("ERR " + err.Error())
		return err
	}
	if aWorkerMACKey == "" {
		err := fmt.Errorf("oauth worker mac key is required")
		report("ERR " + err.Error())
		return err
	}
	macKey, err := hex.DecodeString(aWorkerMACKey)
	if err != nil {
		err = fmt.Errorf("invalid oauth worker mac key: %w", err)
		report("ERR " + err.Error())
		return err
	}
	resp := server.TunnelOpenResponse{
		SessionID:     aWorkerSessionID,
		AttachTimeout: aConnectTimeout,
		IdleTimeout:   aIOTimeout,
	}
	log.Printf("oauth worker started pid=%d session=%s", os.Getpid(), aWorkerSessionID)
	if resp.IdleTimeout <= 0 {
		resp.IdleTimeout = time.Minute
	}
	err = startTunnelClient(resp, macKey, aConnectTimeout, func() error {
		report("OK")
		return nil
	})
	if err != nil {
		log.Printf("oauth worker finished with error: %v", err)
		return err
	}
	log.Printf("oauth worker finished successfully")
	return nil
}
