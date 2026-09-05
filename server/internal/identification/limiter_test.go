package identification_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
)

func TestLimiterSpacesCallsByInterval(t *testing.T) {
	limiter := identification.NewLimiter(40 * time.Millisecond)
	started := time.Now()

	for range 3 {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	}

	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("three calls took %v, want at least two intervals (80ms)", elapsed)
	}
}

func TestLimiterFirstCallDoesNotWait(t *testing.T) {
	limiter := identification.NewLimiter(time.Second)
	started := time.Now()

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("first call waited %v", elapsed)
	}
}

func TestLimiterSerialisesConcurrentCallers(t *testing.T) {
	limiter := identification.NewLimiter(30 * time.Millisecond)
	const callers = 4
	starts := make(chan time.Time, callers)
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			if err := limiter.Wait(context.Background()); err != nil {
				t.Error(err)
			}
			starts <- time.Now()
		})
	}
	group.Wait()
	close(starts)

	var ordered []time.Time
	for start := range starts {
		ordered = append(ordered, start)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Before(ordered[j]) })
	for index := 1; index < len(ordered); index++ {
		if gap := ordered[index].Sub(ordered[index-1]); gap < 25*time.Millisecond {
			t.Fatalf("callers %d and %d started %v apart, want at least the interval", index-1, index, gap)
		}
	}
}

func TestLimiterReturnsSlotOfCancelledLastWaiter(t *testing.T) {
	limiter := identification.NewLimiter(200 * time.Millisecond)
	_ = limiter.Wait(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_ = limiter.Wait(ctx)
	started := time.Now()

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("waited %v behind a cancelled reservation, want about one interval", elapsed)
	}
}

func TestLimiterReturnsWhenContextEnds(t *testing.T) {
	limiter := identification.NewLimiter(time.Second)
	_ = limiter.Wait(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := limiter.Wait(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}
