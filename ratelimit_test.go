package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimit(t *testing.T) {
	t.Run("Standard Rate Limit", func(t *testing.T) {
		expected := time.Duration(15 * time.Second)
		limiter := New(context.Background(), 10, expected)
		require.NotNil(t, limiter)
		var count int
		start := time.Now()
		for i := 0; i < 10; i++ {
			limiter.Take()
			count++
		}
		took := time.Since(start).Nanoseconds()
		require.Equal(t, count, 10)
		require.Less(t, took, expected.Nanoseconds())
		// take another one above max
		limiter.Take()
		took = time.Since(start).Nanoseconds()
		require.GreaterOrEqual(t, took, expected.Nanoseconds())
	})

	t.Run("Unlimited Rate Limit", func(t *testing.T) {
		limiter := NewUnlimited(context.Background())
		require.NotNil(t, limiter)
		var count int
		start := time.Now()
		for i := 0; i < 1000; i++ {
			limiter.Take()
			count++
		}
		took := time.Since(start)
		require.Equal(t, count, 1000)
		require.Lessf(t, took.Nanoseconds(), time.Duration(1*time.Second).Nanoseconds(), "burst rate of unlimited ratelimit is too low")
	})

	t.Run("Concurrent Rate Limit Use", func(t *testing.T) {
		limiter := New(context.Background(), 20, time.Duration(10*time.Second))
		require.NotNil(t, limiter)
		var count int32
		expected := 40
		start := time.Now()
		var wg sync.WaitGroup
		// expected
		// - First burst of 20 go routines at 0 seconds
		// - Second burst of 20 go routines at 10 seconds
		// - Everything should complete right after 10 seconds
		for i := 0; i < expected; i++ {
			wg.Add(1)

			go func() {
				defer wg.Done()

				limiter.Take()
				atomic.AddInt32(&count, 1)
			}()
		}

		wg.Wait()
		limiter.Stop()
		took := time.Since(start)
		require.Equal(t, expected, int(count))
		require.GreaterOrEqualf(t, took, time.Duration(10*time.Second).Nanoseconds(), "ratelimit timeframe mismatch should be > 10s")
		require.LessOrEqualf(t, took, time.Duration(12*time.Second).Nanoseconds(), "ratelimit timeframe mismatch should be < 10s")
	})

	t.Run("Time comparsion", func(t *testing.T) {
		limiter := New(context.TODO(), 100, time.Duration(3)*time.Second)
		// if ratelimit works correctly it should take at least 6 sec to take/consume 201 tokens
		startTime := time.Now()
		for i := 0; i < 201; i++ {
			limiter.Take()
		}
		timetaken := time.Since(startTime)
		expected := time.Duration(6) * time.Second
		require.GreaterOrEqualf(t, timetaken.Nanoseconds(), expected.Nanoseconds(), "more tokens sent than expected with ratelimit")
	})

	t.Run("Test Take and CanTake", func(t *testing.T) {
		limiter := New(context.TODO(), 3, time.Hour)

		require.True(t, limiter.CanTake())
		limiter.Take()
		limiter.Take()
		limiter.Take()
		require.False(t, limiter.CanTake())
	})
}
