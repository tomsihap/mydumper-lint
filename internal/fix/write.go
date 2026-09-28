package fix

import (
	"os"
	"path/filepath"
)

// WriteAtomic replaces the file at path with data (design §7.4): it writes a
// temporary file in the same directory, syncs it, and renames it over the
// target. The mode, and the owner when permitted, are preserved. A symlink is
// resolved and its target is written, so the link stays a link.
func WriteAtomic(path string, data []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := createTemp(dir, "."+filepath.Base(target)+".mydumper-lint-*")
	if err != nil {
		return err
	}
	done, closed := false, false
	defer func() {
		if !done {
			if !closed {
				_ = tmp.Close()
			}
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if f, ok := tmp.(*os.File); ok {
		preserveOwner(f, info)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	done = true
	syncDir(dir)
	return nil
}

// tempFile is the part of *os.File WriteAtomic uses; tests replace
// createTemp to make each step fail.
type tempFile interface {
	Write(b []byte) (int, error)
	Chmod(mode os.FileMode) error
	Sync() error
	Close() error
	Name() string
}

var createTemp = func(dir, pattern string) (tempFile, error) { return os.CreateTemp(dir, pattern) }
