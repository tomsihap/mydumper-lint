//go:build unix

package fix

import (
	"os"
	"syscall"
)

// preserveOwner copies the owner of the original file, when permitted (it
// usually is not for another user's file unless running as root).
func preserveOwner(f *os.File, info os.FileInfo) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
}

// syncDir makes the rename durable.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}
