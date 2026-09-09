package ratelimit

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnlimitedNoResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewUnlimited(context.Background())
		defer limiter.Stop()
		require.Nil(t, limiter.ticker, "unlimited mode must not schedule refills")
		require.Nil(t, limiter.tokens, "unlimited mode must not exchange tokens")
		require.Nil(t, limiter.cancelFunc, "unlimited mode must not start a worker")
		require.Equal(t, uint(math.MaxUint32), limiter.GetLimit())
		for i := 0; i < 1000; i++ {
			limiter.Take()
			require.True(t, limiter.CanTake())
		}
	})
}

func TestUnlimitedLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		limiter := NewUnlimited(ctx)
		cancel()
		limiter.Stop()
		limiter.Stop()
		limiter.Take()
		require.True(t, limiter.CanTake())
	})
}

func TestUnlimitedSetters(t *testing.T) {
	for _, durationFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(durationFirst), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limiter := NewUnlimited(context.Background())
				defer limiter.Stop()
				if durationFirst {
					limiter.SetDuration(time.Second)
					limiter.SetLimit(2)
				} else {
					limiter.SetLimit(2)
					limiter.SetDuration(time.Second)
				}
				require.Equal(t, uint(2), limiter.GetLimit())
				// SetLimit changes the next refill, not the existing burst.
				for i := 0; i < 3; i++ {
					limiter.Take()
				}
				time.Sleep(time.Second)
				synctest.Wait()
				limiter.Take()
				limiter.Take()
				synctest.Wait()
				require.False(t, limiter.CanTake())
				start := time.Now()
				limiter.Take()
				require.Equal(t, time.Second, time.Since(start))
			})
		})
	}
}

func TestUnlimitedSetLimitDefaultDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewUnlimited(context.Background())
		defer limiter.Stop()
		t.Cleanup(func() { limiter.Stop(); time.Sleep(time.Millisecond); synctest.Wait() })
		limiter.SetLimit(1)
		time.Sleep(time.Millisecond)
		synctest.Wait()
		limiter.Take()
		start := time.Now()
		limiter.Take()
		require.Equal(t, time.Millisecond, time.Since(start))
	})
}

func TestUnlimitedSettersAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewUnlimited(context.Background())
		limiter.Stop()
		limiter.SetLimit(0)
		limiter.SetDuration(time.Hour)
		require.Equal(t, uint(0), limiter.GetLimit())
		limiter.Take()
		require.Panics(t, func() { limiter.SetDuration(0) })
		require.Panics(t, func() { limiter.SetDuration(-time.Second) })
	})
}

func TestUnlimitedConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := NewUnlimited(context.Background())
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Go(func() {
				limiter.Take()
				limiter.CanTake()
				limiter.GetLimit()
				limiter.SetLimit(math.MaxUint32)
				limiter.SetDuration(time.Millisecond)
				limiter.Stop()
			})
		}
		wg.Wait()
		limiter.Take()
	})
}

func TestUnlimitedCanceledBeforeConfiguration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		limiter := NewUnlimited(ctx)
		cancel()
		limiter.SetDuration(time.Second)
		synctest.Wait()
		limiter.Take()
		limiter.Stop()
	})
}

func TestUnlimitedWrappers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		multi, err := NewMultiLimiter(context.Background(), &Options{Key: "unlimited", IsUnlimited: true})
		require.NoError(t, err)
		defer multi.Stop()
		auto := NewAutoLimiter(context.Background(), WithUnlimited())
		defer auto.Stop()
		for i := 0; i < 100; i++ {
			require.NoError(t, multi.Take("unlimited"))
			require.True(t, multi.CanTake("unlimited"))
			require.NoError(t, auto.Take("unlimited"))
		}
		direct, err := multi.get("unlimited")
		require.NoError(t, err)
		automatic, err := auto.get("unlimited")
		require.NoError(t, err)
		for _, limiter := range []*Limiter{direct, automatic} {
			require.Nil(t, limiter.ticker)
			require.Nil(t, limiter.tokens)
			require.Nil(t, limiter.cancelFunc)
			require.Equal(t, uint(math.MaxUint32), limiter.GetLimit())
		}
		auto.Stop()
		require.NoError(t, auto.Take("unlimited"))
	})
}
