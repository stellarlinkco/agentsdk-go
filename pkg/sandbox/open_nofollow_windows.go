//go:build windows

package sandbox

// openNoFollow is a best-effort guard against symlink substitution.
//
// Windows does not support syscall.O_NOFOLLOW, so we rely on os.Lstat in
// PathResolver. A TOCTOU gap exists between Lstat and actual file access.
func openNoFollow(path string) error {
	return nil
}

