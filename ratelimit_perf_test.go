package ratelimit

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNoBackgroundGoroutine guards the core of this change: the default limiter
// no longer spawns a background goroutine per instance to serve tokens over a
// channel. Creating many limiters must not grow the goroutine count.
func TestNoBackgroundGoroutine(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	const n = 200
	limiters := make([]*Limiter, 0, n)
	for i := 0; i < n; i++ {
		l := New(context.Background(), 100, time.Second)
		l.Take()
		limiters = append(limiters, l)
	}
	// give any (unexpected) goroutines a chance to show up
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()
	runtime.KeepAlive(limiters)
	require.Less(t, after-before, 20, "creating limiters must not spawn a goroutine each")
}

// TestStopUnblocksTake ensures Stop releases a caller blocked waiting for the
// next window, matching the previous channel-close behavior.
func TestStopUnblocksTake(t *testing.T) {
	l := New(context.Background(), 1, time.Hour)
	l.Take() // drain the only token so the next Take blocks

	done := make(chan struct{})
	go func() {
		l.Take()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	l.Stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not unblock a pending Take")
	}
}

// TestContextCancelUnblocksTake ensures cancelling the context handed to New
// releases a blocked Take.
func TestContextCancelUnblocksTake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	l := New(ctx, 1, time.Hour)
	l.Take()

	done := make(chan struct{})
	go func() {
		l.Take()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("context cancel did not unblock a pending Take")
	}
}

func BenchmarkTakeUnlimited(b *testing.B) {
	l := NewUnlimited(context.Background())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Take()
	}
}

func BenchmarkTakeParallel(b *testing.B) {
	// high rate so the window never blocks within the benchmark
	l := New(context.Background(), 1<<30, time.Second)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Take()
		}
	})
}
