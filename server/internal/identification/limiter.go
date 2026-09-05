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
// order they reserve a slot; a cancelled context releases the caller but
// keeps its reserved slot so later callers stay spaced.
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
		return ctx.Err()
	}
}
