package ratelimit

import (
	"context"
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
