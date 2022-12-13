package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/projectdiscovery/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestMultiLimiter(t *testing.T) {
	limiter := ratelimit.NewMultiLimiter(context.Background())

	// 20 tokens every 1 minute
	err := limiter.Add("default", 20)
	require.Nil(t, err, "failed to add new key")

	before := time.Now()
	// take 21 tokens
	for i := 0; i < 21; i++ {
		err2 := limiter.Take("default")
		require.Nil(t, err2, "failed to take")
	}
	actual := time.Since(before)
	expected := time.Duration(time.Minute)

	require.Greater(t, actual, expected)

}
