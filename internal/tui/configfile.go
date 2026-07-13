package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func acquireConfigLock(path string) (func(), error) {
	writePath, err := configWritePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(writePath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(writePath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// writeFileAtomic replaces a config file with a fully written same-directory
// temporary file. Readers therefore observe either the old or new valid TOML,
// never the transient empty/partial contents of a truncate-and-write update.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	writePath, err := configWritePath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(writePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(writePath)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = fmt.Errorf("short config write: wrote %d of %d bytes", n, len(data))
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, writePath)
}

func configWritePath(path string) (string, error) {
	writePath := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			target, err = os.Readlink(path)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
		}
		writePath = filepath.Clean(target)
	}
	return writePath, nil
}
