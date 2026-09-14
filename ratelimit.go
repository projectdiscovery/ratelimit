package ratelimit

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// equals to -1
var minusOne = ^uint32(0)

// Limiter allows a burst of request during the defined duration
type Limiter struct {
	unlimited *unlimitedLimiter
	strategy  Strategy
	maxCount  atomic.Uint32
	interval  time.Duration
	count     atomic.Uint32
	ticker    *time.Ticker
	tokens    chan struct{}
	ctx       context.Context
	// internal
	cancelFunc context.CancelFunc

	// wraps uber's leaky bucket limiter sizing it to the desired tokens per duration
	leakyBucketLimiter *rate.Limiter
}

// unlimitedLimiter creates a finite bucket only when a caller changes its settings.
// Its mutex protects initialization and Stop; Take never holds it while waiting.
type unlimitedLimiter struct {
	mu      sync.Mutex
	finite  *Limiter
	stopped bool
}

func (u *unlimitedLimiter) bucket() *Limiter {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.finite
}

// Called with unlimited.mu held. Preserve the initial burst until the first refill.
func (limiter *Limiter) initUnlimitedBucket() *Limiter {
	u := limiter.unlimited
	if u.finite == nil && !u.stopped {
		u.finite = New(limiter.ctx, math.MaxUint32, limiter.interval)
		u.finite.SetLimit(limiter.GetLimit())
	}

	return u.finite
}

func (limiter *Limiter) run(ctx context.Context) {
	defer close(limiter.tokens)
	for {
		if limiter.count.Load() == 0 {
			<-limiter.ticker.C
			limiter.count.Store(limiter.maxCount.Load())
		}

		select {
		case <-ctx.Done():
			// Internal Context
			limiter.ticker.Stop()
			return
		case <-limiter.ctx.Done():
			limiter.ticker.Stop()
			return
		case limiter.tokens <- struct{}{}:
			limiter.count.Add(minusOne)
		case <-limiter.ticker.C:
			limiter.count.Store(limiter.maxCount.Load())
		}
	}
}

// Take one token from the bucket
func (limiter *Limiter) Take() {
	if limiter.unlimited != nil {
		if finite := limiter.unlimited.bucket(); finite != nil {
			finite.Take()
		}

		return
	}

	switch limiter.strategy {
	case LeakyBucket:
		_ = limiter.leakyBucketLimiter.Wait(limiter.ctx)
	default:
		<-limiter.tokens
	}
}

// CanTake checks if the rate limiter has any token
func (limiter *Limiter) CanTake() bool {
	if limiter.unlimited != nil {
		if finite := limiter.unlimited.bucket(); finite != nil {
			return finite.CanTake()
		}

		return true
	}

	switch limiter.strategy {
	case LeakyBucket:
		return limiter.leakyBucketLimiter.Tokens() > 0
	default:
		return limiter.count.Load() > 0
	}
}

// GetLimit returns current rate limit per given duration
func (limiter *Limiter) GetLimit() uint {
	return uint(limiter.maxCount.Load())
}

// SetLimit changes the rate limit. Burst buckets apply it at the next refill.
// On an unlimited limiter, it starts a finite bucket with a 1 ms interval unless
// SetDuration has already changed the interval.
func (limiter *Limiter) SetLimit(max uint) {
	if limiter.unlimited != nil {
		limiter.unlimited.mu.Lock()
		defer limiter.unlimited.mu.Unlock()

		limiter.maxCount.Store(uint32(max))

		if finite := limiter.initUnlimitedBucket(); finite != nil {
			finite.SetLimit(max)
		}

		return
	}

	limiter.maxCount.Store(uint32(max))

	switch limiter.strategy {
	case LeakyBucket:
		limiter.leakyBucketLimiter.SetLimit(leakyBucketRate(max, limiter.interval))
		limiter.leakyBucketLimiter.SetBurst(int(max))
	default:
	}
}

// SetDuration changes the refill interval. For burst buckets, it panics if d is not positive.
// On an unlimited limiter, it starts a finite bucket using the current limit.
func (limiter *Limiter) SetDuration(d time.Duration) {
	if limiter.unlimited != nil {
		limiter.unlimited.mu.Lock()
		defer limiter.unlimited.mu.Unlock()

		if d <= 0 {
			panic("non-positive interval for Ticker.Reset")
		}
		limiter.interval = d

		if finite := limiter.initUnlimitedBucket(); finite != nil {
			finite.SetDuration(d)
		}

		return
	}

	limiter.interval = d
	switch limiter.strategy {
	case LeakyBucket:
		limiter.leakyBucketLimiter.SetLimit(leakyBucketRate(limiter.GetLimit(), d))
	default:
		limiter.ticker.Reset(d)
	}
}

// Stop the rate limiter canceling the internal context
func (limiter *Limiter) Stop() {
	if limiter.unlimited != nil {
		limiter.unlimited.mu.Lock()
		defer limiter.unlimited.mu.Unlock()

		limiter.unlimited.stopped = true
		if limiter.unlimited.finite != nil {
			limiter.unlimited.finite.Stop()
		}

		return
	}

	switch limiter.strategy {
	case LeakyBucket:
		if limiter.cancelFunc != nil {
			limiter.cancelFunc()
		}
	default:
		if limiter.cancelFunc != nil {
			limiter.cancelFunc()
		}
	}
}

// New creates a new limiter instance with the tokens amount and the interval
func New(ctx context.Context, max uint, duration time.Duration) *Limiter {
	internalctx, cancel := context.WithCancel(context.TODO())

	maxCount := &atomic.Uint32{}
	maxCount.Store(uint32(max))
	limiter := &Limiter{
		ticker:     time.NewTicker(duration),
		tokens:     make(chan struct{}),
		ctx:        ctx,
		cancelFunc: cancel,
		strategy:   None,
		interval:   duration,
	}

	limiter.maxCount.Store(uint32(max))
	limiter.count.Store(uint32(max))

	go limiter.run(internalctx)

	return limiter
}

// NewUnlimited creates a limiter whose Take never waits and CanTake returns true
// until SetLimit or SetDuration is called.
// It creates no timer, token channel, or background goroutine until SetLimit or
// SetDuration configures a finite bucket. GetLimit initially returns math.MaxUint32.
func NewUnlimited(ctx context.Context) *Limiter {
	limiter := &Limiter{
		unlimited: &unlimitedLimiter{},
		ctx:       ctx,
		interval:  time.Millisecond,
	}

	limiter.maxCount.Store(math.MaxUint32)

	return limiter
}

// NewLeakyBucket creates a limiter that uses golang.org/x/time/rate.
func NewLeakyBucket(ctx context.Context, max uint, duration time.Duration) *Limiter {
	internalctx, cancel := context.WithCancel(ctx)
	limiter := &Limiter{
		strategy:           LeakyBucket,
		leakyBucketLimiter: rate.NewLimiter(leakyBucketRate(max, duration), int(max)),
		ctx:                internalctx,
		cancelFunc:         cancel,
	}

	limiter.maxCount.Store(uint32(max))
	limiter.interval = duration

	return limiter
}

func leakyBucketRate(max uint, duration time.Duration) rate.Limit {
	if duration <= 0 {
		return rate.Inf
	}
	return rate.Limit(float64(max) / duration.Seconds())
}
