package style

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRenderVersion(t *testing.T) {
	got := RenderVersion("prysm", "1.2.3")
	if !strings.Contains(got, "prysm") {
		t.Errorf("RenderVersion should contain app name, got %q", got)
	}
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("RenderVersion should contain version, got %q", got)
	}
	if !strings.Contains(got, "version") {
		t.Errorf("RenderVersion should contain 'version', got %q", got)
	}
}

func TestRenderVersion_Empty(t *testing.T) {
	got := RenderVersion("", "")
	if !strings.Contains(got, "version") {
		t.Errorf("RenderVersion with empty inputs should still contain 'version', got %q", got)
	}
}

func TestRenderer_Stdout(t *testing.T) {
	r := Renderer(os.Stdout)
	if r == nil {
		t.Fatal("Renderer(os.Stdout) returned nil")
	}
}

func TestRenderer_OtherWriter(t *testing.T) {
	var buf bytes.Buffer
	r := Renderer(&buf)
	if r == nil {
		t.Fatal("Renderer(&buf) returned nil")
	}
}

func TestStylesNotNil(t *testing.T) {
	styles := map[string]Style{
		"Title":        Title,
		"Success":      Success,
		"Warning":      Warning,
		"Error":        Error,
		"Info":         Info,
		"MutedStyle":   MutedStyle,
		"Bold":         Bold,
		"Code":         Code,
		"BlueStyle":    BlueStyle,
		"MagentaStyle": MagentaStyle,
		"VersionBox":   VersionBox,
		"WelcomeBox":   WelcomeBox,
		"Tagline":      Tagline,
		"SectionHeader": SectionHeader,
		"HintKey":      HintKey,
	}
	for name, s := range styles {
		// Render an empty string to ensure the style doesn't panic
		got := s.Render("test")
		if got == "" {
			t.Errorf("style %s.Render returned empty", name)
		}
	}
}

func TestSuccessRender(t *testing.T) {
	got := Success.Render("ok")
	if !strings.Contains(got, "ok") {
		t.Errorf("Success.Render should contain text, got %q", got)
	}
}

func TestErrorRender(t *testing.T) {
	got := Error.Render("fail")
	if !strings.Contains(got, "fail") {
		t.Errorf("Error.Render should contain text, got %q", got)
	}
}

func TestWarningRender(t *testing.T) {
	got := Warning.Render("caution")
	if !strings.Contains(got, "caution") {
		t.Errorf("Warning.Render should contain text, got %q", got)
	}
}
