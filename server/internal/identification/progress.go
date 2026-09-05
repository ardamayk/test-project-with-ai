package identification

import "context"

type progressKey struct{}

// WithProgress observes provider queue waits without exposing transport details
// to the import module. Cached responses do not enter the provider queue.
func WithProgress(ctx context.Context, report func(isWaiting bool)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

func reportProgress(ctx context.Context, isWaiting bool) {
	if report, ok := ctx.Value(progressKey{}).(func(bool)); ok {
		report(isWaiting)
	}
}
