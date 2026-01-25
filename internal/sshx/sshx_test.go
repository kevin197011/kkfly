package sshx

import "testing"

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
