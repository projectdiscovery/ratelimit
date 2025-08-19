package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEfficientLimiterNew(t *testing.T) {
	ctx := context.Background()

	// Test with no options
	limiter := NewEfficientLimiter(ctx)
	require.NotNil(t, limiter)
	require.NotNil(t, limiter.defaultOptions)

	// Test with unlimited option
	limiter = NewEfficientLimiter(ctx, WithUnlimited())
	require.NotNil(t, limiter)
	require.True(t, limiter.defaultOptions.IsUnlimited)

	// Test with duration and max count
	limiter = NewEfficientLimiter(ctx, WithDuration(2*time.Second), WithMaxCount(50))
	require.NotNil(t, limiter)
	require.Equal(t, 2*time.Second, limiter.defaultOptions.Duration)
	require.Equal(t, uint(50), limiter.defaultOptions.MaxCount)
	require.False(t, limiter.defaultOptions.IsUnlimited)
}

func TestEfficientLimiterAdd(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Test adding a key with custom options
	err := limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))
	require.NoError(t, err)

	// Test adding the same key again (should fail)
	err = limiter.Add("key1", WithDuration(time.Second), WithMaxCount(20))
	require.Error(t, err)
	require.Equal(t, ErrEfficientKeyAlreadyExists, err)

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

func TestEfficientLimiterTake(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(5))

	// Test taking from non-existent key (should create with defaults)
	err := limiter.Take("key1")
	require.NoError(t, err)

	// Test taking multiple times
	for i := 0; i < 4; i++ {
		err = limiter.Take("key1")
		require.NoError(t, err)
	}

	// Test taking when limit is reached
	err = limiter.Take("key1")
	require.Error(t, err) // Should fail on 6th attempt

	// Test taking from unlimited key
	limiter.Add("unlimited", WithUnlimited())
	for i := 0; i < 100; i++ {
		err = limiter.Take("unlimited")
		require.NoError(t, err)
	}
}

func TestEfficientLimiterCanTake(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(3))

	// Test CanTake on non-existent key (should create with defaults)
	require.True(t, limiter.CanTake("key1"))

	// Take tokens to consume them
	limiter.Take("key1")
	require.True(t, limiter.CanTake("key1"))
	limiter.Take("key1")
	require.True(t, limiter.CanTake("key1"))
	limiter.Take("key1")
	require.True(t, limiter.CanTake("key1"))

	// Test CanTake when limit is reached
	limiter.Take("key1")
	require.False(t, limiter.CanTake("key1"))

	// Test CanTake from unlimited key
	limiter.Add("unlimited", WithUnlimited())
	for i := 0; i < 100; i++ {
		require.True(t, limiter.CanTake("unlimited"))
	}
}

func TestEfficientLimiterAllow(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(2))

	// Test Allow on non-existent key (should create with defaults)
	require.True(t, limiter.Allow("key1"))

	// Take tokens to consume them
	limiter.Take("key1")
	require.True(t, limiter.Allow("key1"))
	limiter.Take("key1")
	require.True(t, limiter.Allow("key1"))

	// Test Allow when limit is reached
	limiter.Take("key1")
	require.False(t, limiter.Allow("key1"))
}

func TestEfficientLimiterAllowN(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(5))

	// Test AllowN on non-existent key (should create with defaults)
	require.True(t, limiter.AllowN("key1", 3))

	// Take tokens to consume them
	limiter.Take("key1")
	limiter.Take("key1")
	limiter.Take("key1")

	// Test AllowN when limit is reached
	require.False(t, limiter.AllowN("key1", 4))

	// Test AllowN with unlimited key
	limiter.Add("unlimited", WithUnlimited())
	require.True(t, limiter.AllowN("unlimited", 1000))
}

func TestEfficientLimiterStop(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add some keys
	limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))
	limiter.Add("key2", WithDuration(time.Second), WithMaxCount(10))

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

func TestEfficientLimiterRemove(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add a key with custom options
	limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))

	// Remove the key completely
	limiter.Remove("key1")

	// Both limiter and options should be gone
	_, err := limiter.get("key1")
	require.Error(t, err)

	// Options should also be removed
	_, exists := limiter.options.Load("key1")
	require.False(t, exists)
}

func TestEfficientLimiterRecreateLimiter(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add a key with custom options
	limiter.Add("key1", WithDuration(500*time.Millisecond), WithMaxCount(5))

	// Stop the limiter
	limiter.Stop("key1")

	// Recreate should work with stored options
	recreated, err := limiter.recreateLimiter("key1")
	require.NoError(t, err)
	require.NotNil(t, recreated)

	// Should be able to use the recreated limiter
	require.True(t, recreated.CanTake())
}

func TestEfficientLimiterCreateOrDefault(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Test creating with defaults for new key
	newLimiter := limiter.createOrDefault("newkey")
	require.NotNil(t, newLimiter)
	require.True(t, newLimiter.CanTake())

	// Test creating with custom options for existing key
	limiter.Add("custom", WithDuration(500*time.Millisecond), WithMaxCount(5))
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

func TestEfficientLimiterAddAndTake(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Test AddAndTake on new key
	err := limiter.AddAndTake("key1", WithDuration(500*time.Millisecond), WithMaxCount(3))
	require.NoError(t, err)

	// Should be able to take 2 more tokens
	require.True(t, limiter.CanTake("key1"))
	limiter.Take("key1")
	require.True(t, limiter.CanTake("key1"))
	limiter.Take("key1")
	require.False(t, limiter.CanTake("key1"))

	// Test AddAndTake on existing key
	err = limiter.AddAndTake("key1", WithDuration(time.Second), WithMaxCount(10))
	require.NoError(t, err)

	// Should still respect the original limit of 3
	require.False(t, limiter.CanTake("key1"))
}

func TestEfficientLimiterMemoryEfficiency(t *testing.T) {
	ctx := context.Background()
	limiter := NewEfficientLimiter(ctx, WithDuration(time.Second), WithMaxCount(10))

	// Add many keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		limiter.Add(key, WithDuration(time.Second), WithMaxCount(5))
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
		require.True(t, limiter.CanTake(key))
	}
}
