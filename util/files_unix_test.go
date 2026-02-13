//go:build linux || darwin

package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPermissionsRegularFile(t *testing.T) {
	dir := t.TempDir()
	fname := filepath.Join(dir, "testfile")
	if err := os.WriteFile(fname, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	// Owner-only permissions, readOK=false -- should pass
	if err := checkPermissions(fname, false); err != nil {
		t.Errorf("expected no error for 0600 with readOK=false, got: %v", err)
	}

	// readOK=true should also pass
	if err := checkPermissions(fname, true); err != nil {
		t.Errorf("expected no error for 0600 with readOK=true, got: %v", err)
	}
}

func TestCheckPermissionsReadOKAllowsGroupRead(t *testing.T) {
	dir := t.TempDir()
	fname := filepath.Join(dir, "testfile")
	if err := os.WriteFile(fname, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 0644 with readOK=true -- should pass (only checks strict perms when readOK=false)
	if err := checkPermissions(fname, true); err != nil {
		t.Errorf("expected no error for 0644 with readOK=true, got: %v", err)
	}
}

func TestCheckPermissionsTooOpen(t *testing.T) {
	dir := t.TempDir()
	fname := filepath.Join(dir, "testfile")
	if err := os.WriteFile(fname, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 0644 with readOK=false -- group/other bits set, should fail
	err := checkPermissions(fname, false)
	if err == nil {
		t.Error("expected error for 0644 with readOK=false")
	}
}

func TestCheckPermissionsWorldWritable(t *testing.T) {
	dir := t.TempDir()
	fname := filepath.Join(dir, "testfile")
	if err := os.WriteFile(fname, []byte("data"), 0666); err != nil {
		t.Fatal(err)
	}

	err := checkPermissions(fname, false)
	if err == nil {
		t.Error("expected error for 0666 with readOK=false")
	}
}

func TestCheckPermissionsNonExistent(t *testing.T) {
	err := checkPermissions("/nonexistent/path/file", false)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCheckPermissionsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Passing a directory should fail (not a regular file)
	err := checkPermissions(dir, false)
	if err == nil {
		t.Error("expected error for directory")
	}
}
