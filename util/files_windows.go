// +build windows

package util

import (
	"fmt"
	"os"
)

func checkPermissions(fname string, perm os.FileMode) (bool, error) {
	fi, err := os.Stat(fname)
	if err != nil || !fi.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file %s", fname)
	}
	// TODO: should we do something here?
	return true, nil
}
