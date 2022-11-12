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
		took := time.Since(start)
		require.Equal(t, count, 10)
		require.True(t, took < expected)
		// take another one above max
		limiter.Take()
		took = time.Since(start)
		require.True(t, took >= expected)
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
		require.True(t, took < time.Duration(1*time.Second))
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
		took := time.Since(start)
		require.Equal(t, expected, int(count))
		require.True(t, took >= time.Duration(10*time.Second))
		require.True(t, took <= time.Duration(12*time.Second))
	})
}
