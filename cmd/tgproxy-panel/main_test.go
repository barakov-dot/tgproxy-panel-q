package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestReadLine(t *testing.T) {
	got, err := readLine(strings.NewReader("hunter2\n"))
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("readLine = %q, want %q", got, "hunter2")
	}
}

// TestRunHashPasswordNonInteractive exercises the path deploy/install.sh
// actually uses: piping a password to `tgproxy-panel -hash-password` over
// stdin (a non-terminal *os.File, via os.Pipe) and capturing stdout.
func TestRunHashPasswordNonInteractive(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString("correct horse battery staple\n"); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	defer r.Close()

	var out, errOut bytes.Buffer
	if err := runHashPassword(r, &out, &errOut); err != nil {
		t.Fatalf("runHashPassword: %v", err)
	}

	hash := strings.TrimSpace(out.String())
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("hash = %q, want $2.. bcrypt prefix", hash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("correct horse battery staple")); err != nil {
		t.Fatalf("generated hash does not verify against the original password: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("errOut = %q, want empty (no prompt expected on a non-terminal stdin)", errOut.String())
	}
}

func TestRunHashPasswordRejectsEmptyPassword(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString("\n"); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	defer r.Close()

	var out, errOut bytes.Buffer
	if err := runHashPassword(r, &out, &errOut); err == nil {
		t.Fatal("runHashPassword: expected an error for an empty password, got nil")
	}
}
