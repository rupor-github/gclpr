package util

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestKeyGrip(t *testing.T) {

	k, err := hex.DecodeString("773E72848C1FD5F9652B29E2E7AF79571A04990E96F2016BF4E0EC1890C2B7DB")
	if err != nil {
		t.Fatal(err)
	}

	var pk [32]byte
	copy(pk[:], k)

	grip := GPGKeyGripED25519(pk)
	gstr := strings.ToUpper(hex.EncodeToString(grip))

	t.Logf("GRIP: %s", gstr)

	if gstr != "9DB6C64A38830F4960701789475520BE8C821F47" {
		t.Fatal("Bad key grip")
	}
}

/*
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
*/
