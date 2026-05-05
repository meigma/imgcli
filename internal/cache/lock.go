package cache

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const lockRetryDelay = 50 * time.Millisecond

// Lock is a held advisory cache lock.
type Lock struct {
	file     *flock.Flock
	unlocked bool
}

// Lock acquires the cache-wide advisory lock.
func (s *DiskStore) Lock(ctx context.Context) (*Lock, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}

	fileLock := flock.New(filepath.Join(s.root, lockFileName), flock.SetPermissions(metadataPerm))
	locked, err := fileLock.TryLockContext(ctx, lockRetryDelay)
	if err != nil {
		return nil, fmt.Errorf("acquire cache lock: %w", err)
	}
	if !locked {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("acquire cache lock: %w", err)
		}
		return nil, errors.New("acquire cache lock")
	}

	return &Lock{file: fileLock}, nil
}

// Unlock releases the cache-wide advisory lock.
func (l *Lock) Unlock() error {
	if l == nil || l.unlocked {
		return nil
	}
	l.unlocked = true
	if err := l.file.Unlock(); err != nil {
		return fmt.Errorf("release cache lock: %w", err)
	}

	return nil
}
