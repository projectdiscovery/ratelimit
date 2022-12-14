package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/projectdiscovery/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestMultiLimiter(t *testing.T) {
	limiter, err := ratelimit.NewMultiLimiter(context.Background(), &ratelimit.Options{
		Key:         "default",
		IsUnlimited: false,
		MaxCount:    100,
		Duration:    time.Duration(3) * time.Second,
	})
	require.Nil(t, err)
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defaultStart := time.Now()
		for i := 0; i < 201; i++ {
			errx := limiter.Take("default")
			require.Nil(t, errx, "failed to take")
		}
		require.Greater(t, time.Since(defaultStart), time.Duration(6)*time.Second)
	}()

	err = limiter.Add(&ratelimit.Options{
		Key:         "one",
		IsUnlimited: false,
		MaxCount:    100,
		Duration:    time.Duration(3) * time.Second,
	})
	require.Nil(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		oneStart := time.Now()
		for i := 0; i < 201; i++ {
			errx := limiter.Take("one")
			require.Nil(t, errx)
		}
		require.Greater(t, time.Since(oneStart), time.Duration(6)*time.Second)
	}()
	wg.Wait()
}
