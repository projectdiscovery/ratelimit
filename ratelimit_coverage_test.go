package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// drainAvailable consumes every currently available token without blocking,
// returning how many were taken (bounded to guard against a runaway loop).
func drainAvailable(l *Limiter) int {
	n := 0
	for n <= 1000 && l.CanTake() {
		l.Take()
		n++
	}
	return n
}

func TestGetSetLimit(t *testing.T) {
	l := New(context.Background(), 5, time.Hour)
	require.Equal(t, uint(5), l.GetLimit())
	l.SetLimit(8)
	require.Equal(t, uint(8), l.GetLimit())
}

// TestRefillBoundedToMax is the guard for the bounded-burst property: after a
// window rolls over the budget refills to exactly max, never accumulating.
func TestRefillBoundedToMax(t *testing.T) {
	const max = 3
	l := New(context.Background(), max, 200*time.Millisecond)

	require.Equal(t, max, drainAvailable(l), "first window must grant exactly max")
	require.False(t, l.CanTake(), "budget must be exhausted within the window")

	// let the window roll over while idle
	time.Sleep(260 * time.Millisecond)

	require.True(t, l.CanTake(), "a rolled-over window must refill")
	require.Equal(t, max, drainAvailable(l), "refill must cap at max, not accumulate idle windows")
}

// TestSetLimitAppliesNextWindow ensures a new limit takes effect once the
// current window refills.
func TestSetLimitAppliesNextWindow(t *testing.T) {
	l := New(context.Background(), 2, 200*time.Millisecond)
	require.Equal(t, 2, drainAvailable(l))

	l.SetLimit(5)
	time.Sleep(260 * time.Millisecond)

	require.Equal(t, 5, drainAvailable(l), "new limit must apply after the window refills")
}

// TestSetDuration ensures shortening the duration shortens the wait for the next
// token instead of leaving the caller blocked on the old interval.
func TestSetDuration(t *testing.T) {
	l := New(context.Background(), 1, time.Hour)
	l.Take() // drain; the next Take would otherwise block ~1h

	l.SetDuration(150 * time.Millisecond)

	start := time.Now()
	l.Take()
	elapsed := time.Since(start)

	require.Less(t, elapsed, time.Second, "SetDuration must shorten the wait")
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "should still wait about the new duration")
}

func TestCanTakeLeakyBucket(t *testing.T) {
	// CanTake on the LeakyBucket strategy reflects the underlying token pool.
	// With a full burst it must report available; the exhausted case is not
	// asserted because rate.Limiter.Tokens() returns a float that creeps back
	// above zero immediately after a take.
	l := NewLeakyBucket(context.Background(), 5, time.Hour)
	require.True(t, l.CanTake())
}

// TestLeakyBucketSetters exercises the LeakyBucket branches of the setters and
// Stop (a no-op for that strategy).
func TestLeakyBucketSetters(t *testing.T) {
	l := NewLeakyBucket(context.Background(), 2, time.Second)
	require.Equal(t, uint(2), l.GetLimit())
	l.SetLimit(5)
	require.Equal(t, uint(5), l.GetLimit())
	l.SetDuration(2 * time.Second)
	l.Stop() // must be a safe no-op for LeakyBucket
}

// TestNonPositiveDurationPanics keeps the ticker contract: a zero or negative
// window would make a zero-rate Take spin without ever checking its context.
func TestNonPositiveDurationPanics(t *testing.T) {
	require.Panics(t, func() { New(context.Background(), 0, 0) })
	require.Panics(t, func() { New(context.Background(), 1, -time.Second) })
	l := New(context.Background(), 1, time.Second)
	require.Panics(t, func() { l.SetDuration(0) })
}

// TestZeroMaxIsUnlimited guards callers that pass a zero limit straight from
// options (for example New(ctx, uint(opts.RateLimit), time.Second) with
// RateLimit unset): Take must never block, as with NewUnlimited.
func TestZeroMaxIsUnlimited(t *testing.T) {
	ctx := context.Background()
	for name, l := range map[string]*Limiter{
		"fixed-window": New(ctx, 0, time.Hour),
		"leaky-bucket": NewLeakyBucket(ctx, 0, time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			defer l.Stop()
			require.Equal(t, uint(0), l.GetLimit())
			done := make(chan error, 1)
			go func() {
				for i := 0; i < 10000; i++ {
					if !l.CanTake() {
						done <- errors.New("CanTake returned false")
						return
					}
					if err := l.TakeContext(ctx); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("Take blocked on a zero limit")
			}
		})
	}
}

// TestSetLimitZeroIsUnlimited checks that setting the limit to 0 at runtime
// lifts the limit, and that a later non-zero limit applies again.
func TestSetLimitZeroIsUnlimited(t *testing.T) {
	l := New(context.Background(), 1, time.Hour)
	defer l.Stop()
	l.Take()
	require.False(t, l.CanTake())

	l.SetLimit(0)
	require.True(t, l.CanTake())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, l.TakeContext(ctx))

	l.SetLimit(1)
	require.False(t, l.CanTake(), "a non-zero limit must apply again")
}
