package util

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"unicode"

	"golang.org/x/crypto/nacl/sign"
)

// ReadKeys returns previously generated key pair (client).
func ReadKeys(home string) (*[32]byte, *[64]byte, error) {

	kd := filepath.Join(home, ".gclpr")
	fi, err := os.Stat(kd)
	if err != nil || !fi.IsDir() {
		return nil, nil, fmt.Errorf("keys directory %s does not exists", kd)
	}

	fn := filepath.Join(kd, "key.pub")
	err = checkPermissions(fn, true)
	if err != nil {
		return nil, nil, fmt.Errorf("public key file permissions are too open: %w", err)
	}
	pubkey, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read public key: %w", err)
	}
	if len(pubkey) != 32 {
		return nil, nil, fmt.Errorf("bad public key size %d", len(pubkey))
	}

	fn = filepath.Join(kd, "key")
	err = checkPermissions(fn, false)
	if err != nil {
		return nil, nil, fmt.Errorf("private key file permissions are too open: %w", err)
	}
	key, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read private key: %w", err)
	}
	if len(key) != 64 {
		return nil, nil, fmt.Errorf("bad private key size %d", len(key))
	}

	var pk [32]byte
	copy(pk[:], pubkey)
	var k [64]byte
	copy(k[:], key)
	return &pk, &k, nil
}

// CreateKeys generates and saves new keypair. If one exists - it will be overwritten (client).
func CreateKeys(home string) (*[32]byte, *[64]byte, error) {

	kd := filepath.Join(home, ".gclpr")
	if err := os.MkdirAll(kd, 0700); err != nil {
		return nil, nil, fmt.Errorf("cannot create keys directory %s: %w", kd, err)
	}

	pk, k, err := sign.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate keys: %w", err)
	}

	err = ioutil.WriteFile(filepath.Join(kd, "key.pub"), pk[:], 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to save public key: %w", err)
	}

	err = ioutil.WriteFile(filepath.Join(kd, "key"), k[:], 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to save private key: %w", err)
	}
	return pk, k, nil
}

// ReadTrustedKeys reads list of trusted public keys from file (server).
func ReadTrustedKeys(home string) (map[[32]byte]struct{}, error) {

	kd := filepath.Join(home, ".gclpr")
	fi, err := os.Stat(kd)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("keys directory %s does not exists", kd)
	}

	fn := filepath.Join(kd, "trusted")
	err = checkPermissions(fn, true)
	if err != nil {
		return nil, fmt.Errorf("trusted keys file permissions are too open: %w", err)
	}
	content, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("unable to read public key: %w", err)
	}

	res := make(map[[32]byte]struct{})
	for _, b := range bytes.Split(bytes.ReplaceAll(content, []byte{'\r'}, []byte{'\n'}), []byte{'\n'}) {
		b = bytes.TrimFunc(b, unicode.IsSpace)
		if len(b) == 0 || b[0] == '#' {
			continue
		}
		l := hex.DecodedLen(len(b))
		if l != 32 {
			log.Printf("Wrong size for key %s... in trusted keys file. Ignoring\n", string(b[:min(8, l)]))
			continue
		}
		dst := make([]byte, l)
		n, err := hex.Decode(dst, b)
		if err != nil {
			log.Printf("Bad key %s... in trusted keys file: %s. Ignoring\n", string(b[:min(8, l)]), err.Error())
			continue
		}
		if n != 32 {
			log.Printf("Wrong size for key %s... in trusted keys file. Ignoring\n", string(b[:min(8, l)]))
			continue
		}
		var k [32]byte
		copy(k[:], dst)
		if _, ok := res[k]; ok {
			log.Printf("Duplicate key %s... in trusted keys file. Ignoring\n", string(b[:8]))
		}
		res[k] = struct{}{}
	}
	return res, nil
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}
