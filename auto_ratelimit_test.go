package ratelimit

import (
	"context"
	"fmt"
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
	require.False(t, recreated.CanTake())
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
