package util

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndReadKeys(t *testing.T) {
	home := t.TempDir()

	// CreateKeys should generate a valid key pair
	pk, k, err := CreateKeys(home)
	if err != nil {
		t.Fatalf("CreateKeys: %v", err)
	}
	if pk == nil || k == nil {
		t.Fatal("CreateKeys returned nil keys")
	}

	// ReadKeys should return the same key pair
	pk2, k2, err := ReadKeys(home)
	if err != nil {
		t.Fatalf("ReadKeys: %v", err)
	}
	if *pk != *pk2 {
		t.Error("public keys do not match")
	}
	if *k != *k2 {
		t.Error("private keys do not match")
	}
}

func TestCreateKeysOverwrites(t *testing.T) {
	home := t.TempDir()

	pk1, _, err := CreateKeys(home)
	if err != nil {
		t.Fatalf("first CreateKeys: %v", err)
	}

	pk2, _, err := CreateKeys(home)
	if err != nil {
		t.Fatalf("second CreateKeys: %v", err)
	}

	if *pk1 == *pk2 {
		t.Error("expected different keys after overwrite")
	}
}

func TestReadKeysNoDirectory(t *testing.T) {
	home := t.TempDir()
	// Don't create .gclpr -- ReadKeys should fail
	_, _, err := ReadKeys(home)
	if err == nil {
		t.Fatal("expected error when .gclpr directory does not exist")
	}
}

func TestReadKeysNotADirectory(t *testing.T) {
	home := t.TempDir()
	// Create .gclpr as a file, not a directory
	if err := os.WriteFile(filepath.Join(home, ".gclpr"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadKeys(home)
	if err == nil {
		t.Fatal("expected error when .gclpr is a file")
	}
}

func TestReadKeysBadSize(t *testing.T) {
	home := t.TempDir()
	kd := filepath.Join(home, ".gclpr")
	if err := os.MkdirAll(kd, 0700); err != nil {
		t.Fatal(err)
	}

	// Write a public key with wrong size
	if err := os.WriteFile(filepath.Join(kd, "key.pub"), []byte("short"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kd, "key"), make([]byte, 64), 0600); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadKeys(home)
	if err == nil {
		t.Fatal("expected error for bad public key size")
	}
}

func TestReadTrustedKeys(t *testing.T) {
	home := t.TempDir()

	// First create a key pair so we have a valid public key
	pk, _, err := CreateKeys(home)
	if err != nil {
		t.Fatalf("CreateKeys: %v", err)
	}

	hexKey := hex.EncodeToString(pk[:])
	trustedContent := "# comment line\n" + hexKey + "\n\n# another comment\n"

	kd := filepath.Join(home, ".gclpr")
	if err := os.WriteFile(filepath.Join(kd, "trusted"), []byte(trustedContent), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := ReadTrustedKeys(home)
	if err != nil {
		t.Fatalf("ReadTrustedKeys: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 trusted key, got %d", len(keys))
	}

	// Verify the key is stored by its sha256 hash
	hk := sha256.Sum256(pk[:])
	stored, ok := keys[hk]
	if !ok {
		t.Fatal("trusted key not found by hash lookup")
	}
	if stored != *pk {
		t.Error("stored key does not match original")
	}
}

func TestReadTrustedKeysMultiple(t *testing.T) {
	home := t.TempDir()
	kd := filepath.Join(home, ".gclpr")
	if err := os.MkdirAll(kd, 0700); err != nil {
		t.Fatal(err)
	}

	// Generate two distinct 32-byte keys
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i)
	}
	for i := range key2 {
		key2[i] = byte(i + 100)
	}

	content := hex.EncodeToString(key1) + "\n" + hex.EncodeToString(key2) + "\n"
	if err := os.WriteFile(filepath.Join(kd, "trusted"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := ReadTrustedKeys(home)
	if err != nil {
		t.Fatalf("ReadTrustedKeys: %v", err)
	}

	if len(keys) != 2 {
		t.Fatalf("expected 2 trusted keys, got %d", len(keys))
	}
}

func TestReadTrustedKeysSkipsInvalid(t *testing.T) {
	home := t.TempDir()
	kd := filepath.Join(home, ".gclpr")
	if err := os.MkdirAll(kd, 0700); err != nil {
		t.Fatal(err)
	}

	validKey := make([]byte, 32)
	for i := range validKey {
		validKey[i] = byte(i)
	}

	content := "not-valid-hex\n" + // bad hex
		"abcd\n" + // wrong size
		hex.EncodeToString(validKey) + "\n" // valid

	if err := os.WriteFile(filepath.Join(kd, "trusted"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := ReadTrustedKeys(home)
	if err != nil {
		t.Fatalf("ReadTrustedKeys: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 valid key (others skipped), got %d", len(keys))
	}
}

func TestReadTrustedKeysNoDirectory(t *testing.T) {
	home := t.TempDir()
	_, err := ReadTrustedKeys(home)
	if err == nil {
		t.Fatal("expected error when .gclpr directory does not exist")
	}
}

func TestReadTrustedKeysCRLF(t *testing.T) {
	home := t.TempDir()
	kd := filepath.Join(home, ".gclpr")
	if err := os.MkdirAll(kd, 0700); err != nil {
		t.Fatal(err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 50)
	}

	// Use CRLF line endings
	content := hex.EncodeToString(key) + "\r\n"
	if err := os.WriteFile(filepath.Join(kd, "trusted"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := ReadTrustedKeys(home)
	if err != nil {
		t.Fatalf("ReadTrustedKeys: %v", err)
	}

	if len(keys) != 1 {
		t.Fatalf("expected 1 key with CRLF endings, got %d", len(keys))
	}
}

func TestZeroBytes(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	ZeroBytes(data)
	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d not zeroed: %d", i, b)
		}
	}
}
