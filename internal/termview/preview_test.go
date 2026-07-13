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

func TestPreviewBytesPreservesAlternateScreenBeforeRestore(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "1049", mode: "1049"},
		{name: "1047", mode: "1047"},
		{name: "47", mode: "47"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte("OLD PRIMARY" +
				"\x1b[?" + tt.mode + "h" +
				"\x1b[2J\x1b[4;1HFINAL CONVERSATION" +
				"\x1b[?" + tt.mode + "l" +
				"\r\nRESTORED PRIMARY")

			got := PreviewBytes(raw, 10, 8, 80)
			if !strings.Contains(got, "FINAL CONVERSATION") {
				t.Fatalf("preview missing final alternate-screen content: %q", got)
			}
			for _, unwanted := range []string{"OLD PRIMARY", "RESTORED PRIMARY"} {
				if strings.Contains(got, unwanted) {
					t.Fatalf("preview contains restored primary-screen content %q: %q", unwanted, got)
				}
			}
		})
	}
}

func TestPreviewBytesUsesCurrentAlternateScreenAfterEarlierRestore(t *testing.T) {
	raw := []byte("\x1b[?1049h\x1b[2JOLD SCREEN\x1b[?1049l" +
		"\x1b[?1049h\x1b[2J\x1b[4;1HCURRENT SCREEN")

	got := PreviewBytes(raw, 10, 8, 80)
	if !strings.Contains(got, "CURRENT SCREEN") {
		t.Fatalf("preview discarded current alternate screen: %q", got)
	}
	if strings.Contains(got, "OLD SCREEN") {
		t.Fatalf("preview retained restored alternate screen: %q", got)
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
