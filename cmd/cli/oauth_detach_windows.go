//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func init() {
	applyWorkerDetach = func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	}
}
