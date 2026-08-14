package gtm

import (
	"errors"
	"time"

	"github.com/nstranquist/ngtm/internal/lockfile"
)

// ErrLaunchLedgerLocked is returned when CopyLocked/WithLock cannot acquire
// the ledger lock before timeout. Same sentinel the appender uses.
var ErrLaunchLedgerLocked = lockfile.ErrLocked

// WithLock runs fn while holding the ledger lock the appender uses, so a
// snapshot copy cannot observe a torn tail.
func (l LaunchLedger) WithLock(timeout time.Duration, fn func() error) error {
	if fn == nil {
		return errors.New("gtm: WithLock nil function")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	err := lockfile.WithFileLockTimeout(l.Path, timeout, fn)
	if errors.Is(err, lockfile.ErrLocked) {
		return ErrLaunchLedgerLocked
	}
	return err
}
