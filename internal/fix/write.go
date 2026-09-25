package fix

import (
	"os"
	"path/filepath"
)

// WriteAtomic replaces the file at path with data (design §7.4): it writes a
// temporary file in the same directory, syncs it, and renames it over the
// target. The mode, and the owner when permitted, are preserved. A symlink is
// resolved and its target is written, so the link stays a link.
func WriteAtomic(path string, data []byte) (err error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".mydumper-lint-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	preserveOwner(tmp, info)
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}
