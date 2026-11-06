package runstate

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ErrStopped is returned when the kill switch is set.
var ErrStopped = errors.New("autopilot: kill switch is set")

// Stopped reports whether the kill switch file exists. Any stat error other
// than "not exist" counts as stopped: when in doubt, do not run.
func (p Paths) Stopped() bool {
	_, err := os.Stat(p.StopFile())
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

// CheckStop returns ErrStopped when the kill switch is set.
func (p Paths) CheckStop() error {
	if p.Stopped() {
		return ErrStopped
	}
	return nil
}

// SetStop sets the kill switch, recording why and when.
func (p Paths) SetStop(reason string, now time.Time) error {
	if err := os.MkdirAll(p.Root, 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("%s %s\n", now.UTC().Format(time.RFC3339), strings.TrimSpace(reason))
	return os.WriteFile(p.StopFile(), []byte(body), 0o600)
}

// ClearStop clears the kill switch. Clearing an unset switch is not an error.
func (p Paths) ClearStop() error {
	err := os.Remove(p.StopFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
