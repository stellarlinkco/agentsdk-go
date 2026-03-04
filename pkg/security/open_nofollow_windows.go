//go:build windows

package security

// openNoFollow is a best-effort guard against symlink substitution.
//
// Windows does not support syscall.O_NOFOLLOW, so we rely on os.Lstat in
// ensureNoSymlink. A TOCTOU gap exists between Lstat and actual file access;
// consider FILE_FLAG_OPEN_REPARSE_POINT via syscall.CreateFile for stronger
// guarantees.
func openNoFollow(path string) error {
	return nil
}
