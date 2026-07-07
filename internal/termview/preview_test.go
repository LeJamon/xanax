package termview

import (
	"bytes"
	"strings"
	"testing"
)

func TestPreviewBytesRendersEscapedScreen(t *testing.T) {
	raw := []byte("\x1b[?1049h\x1b[2J\x1b[1;1HTOPBAR\x1b[40;1HSTATUS")

	got := PreviewBytes(raw, 40, 8, 60)
	if !strings.Contains(got, "STATUS") {
		t.Fatalf("preview missing final screen content: %q", got)
	}
	if bytes.Contains([]byte(got), []byte("\x1b")) {
		t.Fatalf("preview leaked raw escape sequences: %q", got)
	}
}

func TestPreviewBytesUsesLastVisibleRows(t *testing.T) {
	raw := []byte("\x1b[3;1HLINE-A\x1b[4;1HLINE-B\x1b[5;1HLINE-C")

	got := PreviewBytes(raw, 10, 2, 40)
	if got != "LINE-B\nLINE-C" {
		t.Fatalf("preview = %q, want last two visible rows", got)
	}
}

func TestPreviewBytesBlankScreen(t *testing.T) {
	if got := PreviewBytes(nil, 40, 8, 80); got != "" {
		t.Fatalf("blank preview = %q, want empty", got)
	}
}
