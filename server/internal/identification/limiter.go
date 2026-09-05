package identification

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces calls to one external service by a fixed interval so the
// Music Server as a whole stays under MusicBrainz's 1 request/s and
// AcoustID's 3 request/s limits no matter how many uploads run in parallel.
type Limiter struct {
	interval time.Duration
	mutex    sync.Mutex
	nextAt   time.Time
}

func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval}
}

// Wait blocks until the caller may make a request. Callers are served in the
// order they reserve a slot. A caller whose context ends while waiting gives
// its slot back when it was the last reservation, so a cancelled Import
// Batch does not leave later callers idling behind empty slots.
func (limiter *Limiter) Wait(ctx context.Context) error {
	limiter.mutex.Lock()
	now := time.Now()
	startAt := limiter.nextAt
	if startAt.Before(now) {
		startAt = now
	}
	limiter.nextAt = startAt.Add(limiter.interval)
	limiter.mutex.Unlock()

	delay := time.Until(startAt)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		limiter.release(startAt)
		return ctx.Err()
	}
}

func (limiter *Limiter) release(startAt time.Time) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	if limiter.nextAt.Equal(startAt.Add(limiter.interval)) {
		limiter.nextAt = startAt
	}
}
