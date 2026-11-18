package ratelimit

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutoLimiterNew(t *testing.T) {
	ctx := context.Background()

	// Test with no options
	limiter := NewAutoLimiter(ctx)
	require.NotNil(t, limiter)
	require.NotNil(t, limiter.defaultOptions)

	// Test with unlimited option
	limiter = NewAutoLimiter(ctx, WithUnlimited())
	require.NotNil(t, limiter)
	require.True(t, limiter.defaultOptions.IsUnlimited)

	// Test with duration and max count
	limiter = NewAutoLimiter(ctx, WithDuration(2*time.Second), WithMaxCount(50))
	require.NotNil(t, limiter)
	require.Equal(t, 2*time.Second, limiter.defaultOptions.Duration)
	require.Equal(t, uint(50), limiter.defaultOptions.MaxCount)
	require.False(t, limiter.defaultOptions.IsUnlimited)
}

func TestAutoLimiterAdd(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Test adding a key with custom options
	err := limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))
	require.NoError(t, err)

	// Test adding the same key again (should fail)
	err = limiter.Add("key1", WithDuration(time.Second), WithMaxCount(20))
	require.Error(t, err)
	require.Equal(t, ErrAutoKeyAlreadyExists, err)

	// Test adding with empty key
	err = limiter.Add("", WithDuration(time.Second), WithMaxCount(10))
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty keys not allowed")

	// Test adding with zero max count
	err = limiter.Add("key2", WithDuration(time.Second), WithMaxCount(0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "maxcount cannot be zero")

	// Test adding with zero duration
	err = limiter.Add("key3", WithDuration(0), WithMaxCount(10))
	require.Error(t, err)
	require.Contains(t, err.Error(), "time duration not set")

	// Test adding unlimited key
	err = limiter.Add("unlimited", WithUnlimited())
	require.NoError(t, err)
}

func TestAutoLimiterTake(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(5))

	// Test taking from non-existent key (should create with defaults)
	err := limiter.Take("key1")
	require.NoError(t, err)

	// Test taking multiple times
	for i := 0; i < 4; i++ {
		err = limiter.Take("key1")
		require.NoError(t, err)
	}

	// Test taking when limit is reached - this should block, so we test with a timeout
	done := make(chan bool)
	go func() {
		_ = limiter.Take("key1") // This should block
		done <- true
	}()

	// Wait a short time to see if it blocks
	select {
	case <-done:
		t.Fatal("Take() should block when limit is reached")
	case <-time.After(100 * time.Millisecond):
		// Expected behavior - it's blocking
	}

	// Test taking from unlimited key
	_ = limiter.Add("unlimited", WithUnlimited())
	for i := 0; i < 100; i++ {
		err = limiter.Take("unlimited")
		require.NoError(t, err)
	}
}

func TestAutoLimiterStop(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add some keys
	_ = limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))
	_ = limiter.Add("key2", WithDuration(time.Second), WithMaxCount(10))

	// Test stopping specific keys
	limiter.Stop("key1")

	// key1 should be stopped but options preserved
	_, err := limiter.get("key1")
	require.Error(t, err)

	// key2 should still exist
	_, err = limiter.get("key2")
	require.NoError(t, err)

	// Test stopping all keys
	limiter.Stop()

	// All limiters should be stopped
	_, err = limiter.get("key2")
	require.Error(t, err)
}

func TestAutoLimiterRemove(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add a key with custom options
	_ = limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))

	// Remove the key completely
	limiter.Remove("key1")

	// Both limiter and options should be gone
	_, err := limiter.get("key1")
	require.Error(t, err)

	// Options should also be removed
	_, exists := limiter.options.Load("key1")
	require.False(t, exists)
}

func TestAutoLimiterSetRateKeepsWaitersOnTheBucket(t *testing.T) {
	const interval = 100 * time.Millisecond
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(1))
	t.Cleanup(func() { limiter.Stop() })
	require.NoError(t, limiter.Add("key", WithDuration(300*time.Millisecond), WithMaxCount(1)))
	require.NoError(t, limiter.Take("key"))

	type taken struct {
		at  time.Time
		err error
	}
	const waiters = 5
	results := make(chan taken, waiters)
	for range waiters {
		go func() {
			err := limiter.TakeContext(context.Background(), "key")
			results <- taken{time.Now(), err}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	require.NoError(t, limiter.SetRate("key", 1, interval))

	var arrivals []time.Duration
	for range waiters {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			arrivals = append(arrivals, result.at.Sub(start))
		case <-time.After(3 * time.Second):
			t.Fatal("a waiter stayed blocked after SetRate")
		}
	}
	slices.Sort(arrivals)
	// One token a tick of the new interval; timers may wake fractionally early.
	for i, at := range arrivals {
		require.GreaterOrEqualf(t, at, time.Duration(i+1)*interval-10*time.Millisecond, "waiter %d arrived after %v", i+1, at)
	}
	require.Lessf(t, arrivals[waiters-1], waiters*300*time.Millisecond, "waiters kept the old rate")
}

func TestAutoLimiterSetRateCreatesAbsentKey(t *testing.T) {
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(10))
	t.Cleanup(func() { limiter.Stop() })

	require.ErrorContains(t, limiter.SetRate("", 2, time.Hour), "empty keys not allowed")
	require.ErrorContains(t, limiter.SetRate("key", 0, time.Hour), "maxcount cannot be zero")
	require.ErrorContains(t, limiter.SetRate("key", 2, 0), "time duration not set")

	require.NoError(t, limiter.SetRate("key", 2, time.Hour))
	requireTakes := func(n int) {
		t.Helper()
		for range n {
			require.NoError(t, limiter.Take("key"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, limiter.TakeContext(ctx, "key"), context.DeadlineExceeded)
	}
	requireTakes(2)

	// Stop keeps a key's options, so the recreated bucket has the rate set.
	limiter.Stop("key")
	requireTakes(2)
}

func TestAutoLimiterSetRateRacingFirstTake(t *testing.T) {
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(10))
	t.Cleanup(func() { limiter.Stop() })

	for i := range 200 {
		key := fmt.Sprint(i)
		var wg sync.WaitGroup
		var setErr error
		wg.Go(func() { _ = limiter.Take(key) })
		wg.Go(func() { setErr = limiter.SetRate(key, 1, time.Hour) })
		wg.Wait()
		require.NoError(t, setErr)

		bucket, err := limiter.get(key)
		require.NoError(t, err)
		require.EqualValuesf(t, 1, bucket.GetLimit(), "key %s lost its rate to a bucket a Take built", key)
	}
}

func TestAutoLimiterSetRateConcurrently(t *testing.T) {
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(10))
	t.Cleanup(func() { limiter.Stop() })
	require.NoError(t, limiter.Take("key"))

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() { _ = limiter.SetRate("key", uint(i+1), time.Duration(i+1)*time.Second) })
	}
	wg.Wait()

	bucket, err := limiter.get("key")
	require.NoError(t, err)
	stored, _ := limiter.options.Load("key")
	options := stored.(*internalOptions)
	require.Equal(t, options.MaxCount, bucket.GetLimit())
	require.Equal(t, options.Duration, bucket.interval)
}

func TestAutoLimiterTakeContextCancellation(t *testing.T) {
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(1))
	t.Cleanup(func() { limiter.Stop() })
	require.NoError(t, limiter.Take("key"))

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- limiter.TakeContext(ctx, "key") }()
	select {
	case err := <-result:
		t.Fatalf("TakeContext returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestAutoLimiterRemoveTokensNoWaiter(t *testing.T) {
	const interval = 50 * time.Millisecond
	limiter := NewAutoLimiter(context.Background(), WithDuration(time.Hour), WithMaxCount(1))
	require.NoError(t, limiter.Add("key", WithDuration(interval), WithMaxCount(1)))
	require.NoError(t, limiter.Take("key"))
	start := time.Now()

	const waiters = 20
	results := make(chan error, waiters)
	for range waiters {
		go func() { results <- limiter.Take("key") }()
	}
	time.Sleep(20 * time.Millisecond)
	limiter.Remove("key")

	tokened := 0
	for range waiters {
		select {
		case err := <-results:
			if err == nil {
				tokened++
				continue
			}
			require.ErrorIs(t, err, ErrLimiterStopped)
		case <-time.After(2 * time.Second):
			t.Fatal("a waiter stayed blocked after Remove")
		}
	}
	ticks := int(time.Since(start)/interval) + 1
	require.LessOrEqualf(t, tokened, ticks, "%d waiters returned nil, but the bucket ticked only %d times", tokened, ticks)
}

func TestAutoLimiterRecreateLimiter(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add a key with custom options
	_ = limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))

	// Stop the limiter
	limiter.Stop("key1")

	// Recreate should work with stored options
	recreated, err := limiter.recreateLimiter("key1")
	require.NoError(t, err)
	require.NotNil(t, recreated)

	// Should be able to use the recreated limiter
	require.True(t, recreated.CanTake())
}

func TestAutoLimiterCreateOrDefault(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Test creating with defaults for new key
	newLimiter := limiter.createOrDefault("newkey")
	require.NotNil(t, newLimiter)
	require.True(t, newLimiter.CanTake())

	// Test creating with custom options for existing key
	_ = limiter.Add("custom", WithDuration(500*time.Millisecond), WithMaxCount(5))
	limiter.Stop("custom")

	// Should recreate with custom options, not defaults
	recreated := limiter.createOrDefault("custom")
	require.NotNil(t, recreated)

	// Should be able to take 5 tokens (custom limit), not 10 (default)
	for i := 0; i < 5; i++ {
		require.True(t, recreated.CanTake())
		recreated.Take()
	}
	// Take returns at the unbuffered handoff; the producer decrements its
	// accounting immediately afterwards. Wait for that scheduler-sized window
	// instead of making the test depend on goroutine ordering.
	require.Eventually(t, func() bool { return !recreated.CanTake() }, time.Second, time.Millisecond)
}

func TestAutoLimiterAddAndTake(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(100*time.Millisecond), WithMaxCount(10))

	// Test AddAndTake on new key
	err := limiter.AddAndTake("key1", WithDuration(100*time.Millisecond), WithMaxCount(3))
	require.NoError(t, err)

	// Test AddAndTake on existing key (should just take a token, not change the limit)
	err = limiter.AddAndTake("key1", WithDuration(time.Second), WithMaxCount(10))
	require.NoError(t, err)
}

func TestAutoLimiterMemoryEfficiency(t *testing.T) {
	ctx := context.Background()
	limiter := NewAutoLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add many keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		_ = limiter.Add(key, WithDuration(time.Second), WithMaxCount(5))
	}

	// Stop some keys
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		limiter.Stop(key)
	}

	// Remove some keys completely
	for i := 50; i < 75; i++ {
		key := fmt.Sprintf("key%d", i)
		limiter.Remove(key)
	}

	// Check that stopped keys still have options stored
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		_, exists := limiter.options.Load(key)
		require.True(t, exists)
	}

	// Check that removed keys have no options
	for i := 50; i < 75; i++ {
		key := fmt.Sprintf("key%d", i)
		_, exists := limiter.options.Load(key)
		require.False(t, exists)
	}

	// Check that active keys still work
	for i := 75; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		// Just test that we can take a token (this will create the limiter if it doesn't exist)
		err := limiter.Take(key)
		require.NoError(t, err)
	}
}
