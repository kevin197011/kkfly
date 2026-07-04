package runner

import (
	"strings"
	"testing"
	"time"
)

func TestSummarizeCommand(t *testing.T) {
	got := summarizeCommand("echo hello\nworld", 80)
	if !strings.Contains(got, "(2 lines)") {
		t.Fatalf("expected line count hint, got %q", got)
	}
}

func TestFormatFinishedMessage(t *testing.T) {
	got := formatFinishedMessage(0, 1200*time.Millisecond, "")
	want := "exit 0 · 1.2s"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	got = formatFinishedMessage(1, 500*time.Millisecond, "boom")
	if !strings.Contains(got, "exit 1") || !strings.Contains(got, "boom") {
		t.Fatalf("unexpected fail message: %q", got)
	}
}

func TestOverallLabel(t *testing.T) {
	if overallLabel("partial_failure") != "PARTIAL" {
		t.Fatal("expected PARTIAL")
	}
}

func TestOneLineTSVCell(t *testing.T) {
	got := oneLineTSVCell("a\tb\nc")
	if got != "a b c" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitDoneProgress(t *testing.T) {
	pfx, rest := splitDoneProgress("[2/5]  exit 0 · 1s")
	if pfx != "[2/5]  " || rest != "exit 0 · 1s" {
		t.Fatalf("pfx=%q rest=%q", pfx, rest)
	}
}
