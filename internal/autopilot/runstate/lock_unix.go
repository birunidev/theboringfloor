//go:build unix

package runstate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// ErrLocked: another autopilot already holds the lock.
var ErrLocked = errors.New("autopilot: another instance is running")

// Lock is the single-instance lock. The kernel drops it when the process
// dies, so a crash never leaves a stale lock behind.
type Lock struct{ f *os.File }

// AcquireLock takes the exclusive lock at path or returns ErrLocked.
func AcquireLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &Lock{f: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
