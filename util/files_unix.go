//go:build linux || darwin

package util

import (
	"fmt"
	"os"
	"syscall"
)

// checkPermissions verifies that the file has acceptable ownership and permissions.
// When readOK is false, the file must not be readable by group or others.
func checkPermissions(fname string, readOK bool) error {

	fi, err := os.Stat(fname)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file %s", fname)
	}

	var uid int
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		uid = int(stat.Uid)
	}
	perm := fi.Mode().Perm()
	if !readOK && uid == os.Getuid() && (perm&077) != 0 {
		return fmt.Errorf("bad permissions %o for file %s", perm, fname)
	}
	return nil
}
