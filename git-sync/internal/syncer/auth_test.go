package syncer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"

	"git-suite/git-sync/internal/codes"
)

func TestCheckCredentials(t *testing.T) {
	key := testKey(t, "")
	ok := Options{
		URL:                 "ssh://git@github.com/org/repo.git",
		Path:                t.TempDir() + "/dest",
		SSHKey:              key,
		InsecureSkipHostKey: true,
	}
	if err := Check(ok); err != nil {
		t.Fatal(err)
	}

	missingHost := ok
	missingHost.InsecureSkipHostKey = false
	expectCode(t, Check(missingHost), codes.HostKeyConfig)

	locked := ok
	locked.SSHKey = testKey(t, "secret")
	expectCode(t, Check(locked), codes.SSHKey)

	httpOnlyUser := Options{URL: "https://github.com/org/repo.git", Path: "dest", Username: "u"}
	expectCode(t, Check(httpOnlyUser), codes.BadCredentials)

	fileWithPassword := Options{URL: "file:///tmp/repo", Path: "dest", Password: "secret", Username: "u"}
	expectCode(t, Check(fileWithPassword), codes.BadCredentials)
}

func expectCode(t *testing.T, err error, code int) {
	t.Helper()
	var ce *codes.Error
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want code %d", err, code)
	}
	if ce.Code != code {
		t.Fatalf("code = %d (%s), want %d", ce.Code, ce.Message, code)
	}
}

func testKey(t *testing.T, passphrase string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}
