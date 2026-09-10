package vcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLockTimeout is returned when an exclusive state-directory lock cannot be
// acquired before the deadline.
var ErrLockTimeout = errors.New("timed out acquiring vcs state-dir lock")

// lockFileName is the cross-process advisory lock for a VCS state directory.
// It lives directly under the vcs/ baseDir so that every Manager sharing the
// same stateDir contends on the same path.
const lockFileName = "LOCK"

// lockPollInterval is how often a contended acquirer retries.
const lockPollInterval = 20 * time.Millisecond

// lockStaleAfter bounds how long a lock held by a crashed process is honored
// before it is considered abandoned and reclaimed. It is intentionally much
// larger than any realistic critical section (commit/checkout/GC on a normal
// work tree) so that a slow-but-alive holder is never preempted.
const lockStaleAfter = 10 * time.Minute

// LockTimeout is the default upper bound for waiting on the state-dir lock.
// Callers that must not block (best-effort audit paths) can use
// TryWithStateLock instead.
const LockTimeout = 30 * time.Second

// lockRecord is the on-disk lock payload. PID and Timestamp make the lock
// diagnosable and let a later acquirer reclaim a lock whose holder died
// without unlinking it.
type lockRecord struct {
	PID       int       `json:"pid"`
	Timestamp time.Time `json:"timestamp"`
}

// stateLock is an acquired cross-process exclusive lock over a state directory.
type stateLock struct {
	path string
	held bool
}

// lockPath returns the lock file location for this manager.
func (v *Manager) lockPath() string {
	return filepath.Join(v.baseDir, lockFileName)
}

// acquireStateLock takes the cross-process exclusive lock for v.baseDir,
// waiting up to timeout. It returns ErrLockTimeout if the lock stays
// contended, or a wrapped error on an unexpected filesystem failure.
//
// The lock is a directory-visible file created with O_CREATE|O_EXCL, which is
// atomic on both POSIX and Windows. It is advisory: it only coordinates
// processes that go through a Manager. A lock older than lockStaleAfter whose
// owner PID is no longer alive is reclaimed, so a crashed holder cannot wedge
// the state directory forever.
func (v *Manager) acquireStateLock(timeout time.Duration) (*stateLock, error) {
	if err := os.MkdirAll(v.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create vcs base dir: %w", err)
	}
	path := v.lockPath()
	deadline := time.Now().Add(timeout)

	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			rec, _ := json.Marshal(lockRecord{PID: os.Getpid(), Timestamp: time.Now()})
			_, _ = f.Write(rec)
			_ = f.Close()
			return &stateLock{path: path, held: true}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to create lock %s: %w", path, err)
		}

		// Lock is held by someone. Reclaim only if it is both old and its
		// owner is gone; otherwise keep waiting.
		if v.reclaimIfStale(path) {
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrLockTimeout, path)
		}
		time.Sleep(lockPollInterval)
	}
}

// reclaimIfStale removes a lock file that is older than lockStaleAfter and
// whose recorded owner PID is no longer running. It returns true if a lock was
// removed (so the caller should retry immediately). Removal is best-effort: if
// another contender wins the race, it simply finds the lock absent next round.
func (v *Manager) reclaimIfStale(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var rec lockRecord
	if json.Unmarshal(data, &rec) != nil {
		// Unreadable payload: fall back to the file's own mtime.
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) < lockStaleAfter {
			return false
		}
		_ = os.Remove(path)
		return true
	}
	if time.Since(rec.Timestamp) < lockStaleAfter {
		return false
	}
	if rec.PID > 0 && processAlive(rec.PID) {
		return false
	}
	_ = os.Remove(path)
	return true
}

// release drops the lock. It is safe to call on a nil or already-released lock.
func (l *stateLock) release() {
	if l == nil || !l.held {
		return
	}
	l.held = false
	_ = os.Remove(l.path)
}

// withStateLock runs fn while holding the cross-process state-dir lock, using
// the default LockTimeout. The in-process mutex is expected to be held by the
// caller for operations that also mutate Manager-local state.
func (v *Manager) withStateLock(fn func() error) error {
	return v.withStateLockTimeout(LockTimeout, fn)
}

// withStateLockTimeout is withStateLock with an explicit timeout.
func (v *Manager) withStateLockTimeout(timeout time.Duration, fn func() error) error {
	lock, err := v.acquireStateLock(timeout)
	if err != nil {
		return err
	}
	defer lock.release()
	return fn()
}
