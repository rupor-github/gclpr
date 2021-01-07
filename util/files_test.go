package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckWin(t *testing.T) {

	var err error
	home, _ := os.UserHomeDir()

	err = checkPermissions(filepath.Join(home, ".gclpr", "trusted"), true)
	if err != nil {
		t.Fatalf("ERROR: %s", err)
	}
	err = checkPermissions(filepath.Join("D:/", "trusted"), true)
	if err != nil {
		t.Logf("EXPECTED ERROR: %s", err)
	} else {
		t.Fatal("NOT OK")
	}
	t.Log("OK")
}
