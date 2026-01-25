package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeAndValidate_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_test")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cfg := Config{
		User:           "ubuntu",
		PrivateKeyPath: keyPath,
		Command:        "echo hello",
		Port:           22,
		Hosts:          []string{"example.com"},
		Concurrency:    5,
	}
	if err := cfg.NormalizeAndValidate(dir); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestNormalizeAndValidate_InvalidConcurrency(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_test")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cfg := Config{
		User:           "ubuntu",
		PrivateKeyPath: keyPath,
		Command:        "echo hello",
		Port:           22,
		Hosts:          []string{"example.com"},
		Concurrency:    0,
	}
	if err := cfg.NormalizeAndValidate(dir); err == nil {
		t.Fatalf("expected error for invalid concurrency")
	}
}

func TestNormalizeAndValidate_KeyContentOnly(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{
		User:              "ubuntu",
		PrivateKeyContent: "-----BEGIN OPENSSH PRIVATE KEY-----\ninvalid\n-----END OPENSSH PRIVATE KEY-----\n",
		Command:           "echo hello",
		Port:              22,
		Hosts:             []string{"example.com"},
		Concurrency:       1,
	}
	// Config validation only checks presence (not parse-ability). Parsing happens during SSH dial.
	if err := cfg.NormalizeAndValidate(dir); err != nil {
		t.Fatalf("expected valid config when private_key_content is present, got error: %v", err)
	}
}

func TestNormalizeAndValidate_MissingKey(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{
		User:        "ubuntu",
		Command:     "echo hello",
		Port:        22,
		Hosts:       []string{"example.com"},
		Concurrency: 1,
	}
	if err := cfg.NormalizeAndValidate(dir); err == nil {
		t.Fatalf("expected error when no key material is provided")
	}
}
