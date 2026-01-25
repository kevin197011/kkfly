package sshx

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSingleQuoteForBash_NoQuotes(t *testing.T) {
	got := SingleQuoteForBash("hello world")
	want := "'hello world'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSingleQuoteForBash_WithSingleQuote(t *testing.T) {
	got := SingleQuoteForBash("abc'def")
	want := `'abc'"'"'def'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildHostKeyCallback_StrictDisabled_IgnoresKnownHosts(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	cb, err := buildHostKeyCallback(ExecConfig{
		KnownHostsPath:        khPath,
		StrictHostKeyChecking: false,
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if err := cb("10.0.0.1:22", &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 22}, signer.PublicKey()); err != nil {
		t.Fatalf("expected callback to accept unknown host key when strict disabled, got: %v", err)
	}
}
