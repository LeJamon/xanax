package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lease serializes ownership of a session between its supervisor and removal.
// The lease must remain held until the supervisor exits or the session row has
// been deleted.
type Lease struct {
	file *os.File
}

// TryAcquireLease attempts to exclusively own the supervisor lease associated
// with socketPath. ErrAlreadySupervised means another process currently owns
// it; all other errors are filesystem or locking failures.
func TryAcquireLease(socketPath string) (*Lease, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(socketPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadySupervised
		}
		return nil, fmt.Errorf("lock supervisor lease: %w", err)
	}
	return &Lease{file: f}, nil
}

// Release relinquishes the lease. It is safe to call more than once.
func (l *Lease) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}
