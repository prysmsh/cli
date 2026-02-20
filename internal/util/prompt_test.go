package util

import (
	"os"
	"testing"
)

func TestPromptInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("  answer  \n")
		w.Close()
	}()

	got, err := PromptInput("Label")
	if err != nil {
		t.Fatalf("PromptInput err = %v", err)
	}
	if got != "answer" {
		t.Errorf("PromptInput() = %q, want answer", got)
	}
}

func TestPromptInputNoInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	w.Close() // EOF immediately

	_, err = PromptInput("Label")
	if err == nil {
		t.Error("PromptInput() with empty input expected error")
	}
}

func TestPromptPassword(t *testing.T) {
	// When not a terminal, PromptPassword falls back to ReadString from stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("secret\n")
		w.Close()
	}()

	got, err := PromptPassword("Password")
	if err != nil {
		t.Fatalf("PromptPassword err = %v", err)
	}
	if got != "secret" {
		t.Errorf("PromptPassword() = %q, want secret", got)
	}
}

func TestPromptConfirmDefaultNo(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("\n")
		w.Close()
	}()

	got, err := PromptConfirm("Continue?", false)
	if err != nil {
		t.Fatalf("PromptConfirm err = %v", err)
	}
	if got {
		t.Error("PromptConfirm(..., false) with empty input want false")
	}
}

func TestPromptConfirmDefaultYes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("\n")
		w.Close()
	}()

	got, err := PromptConfirm("Continue?", true)
	if err != nil {
		t.Fatalf("PromptConfirm err = %v", err)
	}
	if !got {
		t.Error("PromptConfirm(..., true) with empty input want true")
	}
}

func TestPromptConfirmYes(t *testing.T) {
	for _, input := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdin := os.Stdin
		os.Stdin = r
		go func() {
			w.WriteString(input)
			w.Close()
		}()

		got, err := PromptConfirm("Continue?", false)
		os.Stdin = oldStdin
		if err != nil {
			t.Fatalf("PromptConfirm(%q) err = %v", input, err)
		}
		if !got {
			t.Errorf("PromptConfirm(%q) = false, want true", input)
		}
	}
}

func TestPromptConfirmNo(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	got, err := PromptConfirm("Continue?", true)
	if err != nil {
		t.Fatalf("PromptConfirm err = %v", err)
	}
	if got {
		t.Error("PromptConfirm with n want false")
	}
}
