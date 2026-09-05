package identification_test

import (
	"context"
	"errors"
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

	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("first call waited %v", elapsed)
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
