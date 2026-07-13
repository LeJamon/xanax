package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteFileAtomicNeverExposesPartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := bytes.Repeat([]byte("old-config\n"), 4096)
	newer := bytes.Repeat([]byte("new-config\n"), 4096)
	if err := writeFileAtomic(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	readerDone := make(chan struct{})
	readErr := make(chan error, 1)
	var reads atomic.Int32
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				readErr <- err
				return
			}
			reads.Add(1)
			if !bytes.Equal(data, old) && !bytes.Equal(data, newer) {
				readErr <- &partialConfigError{size: len(data)}
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		data := old
		if i%2 == 0 {
			data = newer
		}
		if err := writeFileAtomic(path, data, 0o600); err != nil {
			close(done)
			t.Fatal(err)
		}
	}
	close(done)
	<-readerDone
	select {
	case err := <-readErr:
		t.Fatal(err)
	default:
	}
	if reads.Load() == 0 {
		t.Fatal("concurrent reader did not observe the config file")
	}
}

func TestWriteFileAtomicPreservesConfigSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed-config.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("atomic config write replaced the symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("symlink target = %q, want new", data)
	}
}

func TestConfigLockSerializesWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	unlockFirst, err := acquireConfigLock(path)
	if err != nil {
		t.Fatal(err)
	}
	type lockResult struct {
		unlock func()
		err    error
	}
	acquired := make(chan lockResult, 1)
	go func() {
		unlock, err := acquireConfigLock(path)
		acquired <- lockResult{unlock: unlock, err: err}
	}()
	select {
	case result := <-acquired:
		unlockFirst()
		if result.unlock != nil {
			result.unlock()
		}
		t.Fatalf("second writer acquired held config lock: %v", result.err)
	case <-time.After(50 * time.Millisecond):
	}
	unlockFirst()
	select {
	case result := <-acquired:
		if result.err != nil {
			t.Fatal(result.err)
		}
		result.unlock()
	case <-time.After(time.Second):
		t.Fatal("second writer did not acquire released config lock")
	}
}

type partialConfigError struct{ size int }

func (e *partialConfigError) Error() string {
	return "reader observed partial config with size " + strconv.Itoa(e.size)
}
