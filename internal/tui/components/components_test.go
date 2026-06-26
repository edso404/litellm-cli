package components

import (
	"strings"
	"testing"
)

func TestHeader_View(t *testing.T) {
	EnsureColorProfile()

	h := NewHeader("Test Title", "Status Info")

	// Test standard wide layout
	v := h.View(40)
	if !strings.Contains(v, "Test Title") || !strings.Contains(v, "Status Info") {
		t.Fatalf("expected title and status to be present in wide view, got: %q", v)
	}

	// Test narrow layout truncation
	vNarrow := h.View(10)
	if len(vNarrow) == 0 {
		t.Fatal("expected non-empty rendered output even when narrow")
	}
}

func TestHelp_View(t *testing.T) {
	EnsureColorProfile()

	keys := []HelpKey{
		{Key: "Tab", Desc: "Switch"},
		{Key: "q", Desc: "Quit"},
	}
	h := NewHelp(keys)

	// Test standard view
	v := h.View(60)
	if !strings.Contains(v, "Tab: Switch") || !strings.Contains(v, "q: Quit") {
		t.Fatalf("expected help keys in wide view, got: %q", v)
	}

	// Test extreme truncation
	vNarrow := h.View(12)
	if !strings.HasSuffix(StripAnsiForTest(vNarrow), "...") {
		t.Fatalf("expected help view to end with ellipsis when narrow, got: %q", vNarrow)
	}
}

func TestErrorBanner_View(t *testing.T) {
	EnsureColorProfile()

	eb := NewErrorBanner("Something failed")
	v := eb.View(50)

	if !strings.Contains(v, "Something failed") || !strings.Contains(v, "❌") {
		t.Fatalf("expected error banner to contain error message and icon, got: %q", v)
	}
}

func TestLoaderAndPlaceholder_View(t *testing.T) {
	EnsureColorProfile()

	l := NewLoader("Loading data")
	lv := l.View()
	if !strings.Contains(lv, "⏳ Loading data") {
		t.Fatalf("unexpected loader rendering: %q", lv)
	}

	p := NewPlaceholder("No results")
	pv := p.View()
	if !strings.Contains(pv, "📦 No results") {
		t.Fatalf("unexpected placeholder rendering: %q", pv)
	}
}

// Simple ANSI stripper helper for assertions
func StripAnsiForTest(s string) string {
	var sb strings.Builder
	inAnsi := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inAnsi = true
			continue
		}
		if inAnsi {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				inAnsi = false
			}
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}
