package output

import (
	"errors"
	"io/fs"
	"syscall"
)

// libuv's wording, which is what Node puts in an fs error's message. Several
// differ from the C library's strerror that Go uses — EISDIR is "illegal
// operation on a directory", not "is a directory".
var libuvMessages = map[syscall.Errno][2]string{
	syscall.ENOENT:       {"ENOENT", "no such file or directory"},
	syscall.EACCES:       {"EACCES", "permission denied"},
	syscall.EPERM:        {"EPERM", "operation not permitted"},
	syscall.EISDIR:       {"EISDIR", "illegal operation on a directory"},
	syscall.ENOTDIR:      {"ENOTDIR", "not a directory"},
	syscall.EEXIST:       {"EEXIST", "file already exists"},
	syscall.EROFS:        {"EROFS", "read-only file system"},
	syscall.ENOSPC:       {"ENOSPC", "no space left on device"},
	syscall.EMFILE:       {"EMFILE", "too many open files"},
	syscall.ELOOP:        {"ELOOP", "too many symbolic links encountered"},
	syscall.ENAMETOOLONG: {"ENAMETOOLONG", "name too long"},
	syscall.EBUSY:        {"EBUSY", "resource busy or locked"},
	syscall.EINVAL:       {"EINVAL", "invalid argument"},
	syscall.EIO:          {"EIO", "i/o error"},
}

// NodeFSError renders a filesystem error as Node's message for it —
// `ENOENT: no such file or directory, open 'x.csv'` — so `import` and
// `report --out` failures read identically from either build.
//
// syscall is Node's name for the operation ("open", "mkdir", "read"), and
// path the path Node would name, which is not always the one Go's error
// carries: a recursive mkdir names the target, not the component that failed,
// and a read names nothing at all because Node reads by descriptor.
func NodeFSError(err error, syscallName, path string) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			errno = syscall.ENOENT
		case errors.Is(err, fs.ErrPermission):
			errno = syscall.EACCES
		case errors.Is(err, fs.ErrExist):
			errno = syscall.EEXIST
		default:
			return err.Error()
		}
	}
	names, ok := libuvMessages[errno]
	if !ok {
		return err.Error()
	}
	msg := names[0] + ": " + names[1] + ", " + syscallName
	if path != "" {
		msg += " '" + path + "'"
	}
	return msg
}
