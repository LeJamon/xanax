package attach

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const resultFDTestMode = "RVR_ATTACH_RESULT_TEST_MODE"

func TestReportResultWritesConfiguredDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	t.Setenv(ResultFDEnv, strconv.Itoa(int(w.Fd())))

	ReportResult(SessionExited)
	_ = w.Close() // mark the original os.File closed after ReportResult owned fd
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := ParseResult(data)
	if !ok || result != SessionExited {
		t.Fatalf("reported result = (%v, %v), want SessionExited", result, ok)
	}
}

func TestParseResultRejectsUnknownToken(t *testing.T) {
	if _, ok := ParseResult([]byte("unknown")); ok {
		t.Fatal("ParseResult accepted an unknown token")
	}
}

func TestProtectResultFDPreventsDescendantInheritance(t *testing.T) {
	switch os.Getenv(resultFDTestMode) {
	case "helper":
		ProtectResultFD()
		descendant := exec.Command(os.Args[0], "-test.run=^TestProtectResultFDPreventsDescendantInheritance$")
		descendant.Env = resultTestEnv(os.Environ(), resultFDTestMode, "descendant")
		if err := descendant.Start(); err != nil {
			t.Fatal(err)
		}
		ReportResult(Detached)
		return
	case "descendant":
		time.Sleep(time.Second)
		return
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	help := exec.Command(os.Args[0], "-test.run=^TestProtectResultFDPreventsDescendantInheritance$")
	help.ExtraFiles = []*os.File{w}
	help.Env = resultTestEnv(os.Environ(), resultFDTestMode, "helper")
	help.Env = resultTestEnv(help.Env, ResultFDEnv, "3")
	if err := help.Start(); err != nil {
		_ = w.Close()
		t.Fatal(err)
	}
	_ = w.Close()

	type readResult struct {
		data []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(r)
		readDone <- readResult{data: data, err: err}
	}()
	select {
	case got := <-readDone:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if result, ok := ParseResult(got.data); !ok || result != Detached {
			t.Fatalf("reported result = (%v, %v), want Detached", result, ok)
		}
	case <-time.After(300 * time.Millisecond):
		_ = help.Process.Kill()
		_ = help.Wait()
		t.Fatal("result pipe remained open in a long-lived descendant")
	}
	if err := help.Wait(); err != nil {
		t.Fatalf("helper process: %v", err)
	}
}

func resultTestEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
