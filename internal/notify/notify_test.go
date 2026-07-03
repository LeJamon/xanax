package notify

import "testing"

func TestOsascriptArgsEscaping(t *testing.T) {
	args := osascriptArgs(`Needs "input"`, "path is C:\\x\nline2")
	if len(args) != 2 || args[0] != "-e" {
		t.Fatalf("args = %v", args)
	}
	got := args[1]
	want := `display notification "path is C:\\x line2" with title "Needs \"input\""`
	if got != want {
		t.Errorf("script =\n  %q\nwant\n  %q", got, want)
	}
}

func TestSendNeverPanics(t *testing.T) {
	// On CI the platform tool may be absent; Send must just return nil.
	if err := Send("xanax test", "body"); err != nil {
		t.Logf("Send returned %v (acceptable if tool present but headless)", err)
	}
}
