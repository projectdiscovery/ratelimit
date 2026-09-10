package ratelimit

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// Limiter allows a burst of request during the defined duration
//
// The default (None) strategy is a fixed-window limiter: up to maxCount tokens
// are available per interval and the budget is refilled in a burst when the
// window rolls over. It is served entirely from a small mutex-guarded counter,
// so Take is a couple of field operations on the hot path. The previous design
// delivered one token per call over an unbuffered channel fed by a dedicated
// background goroutine, which charged every caller a goroutine handoff and
// scheduler wakeup and spawned a goroutine per limiter. That overhead dominates
// once tools push tens of thousands of Take calls per second across many
// workers, so it has been removed.
type Limiter struct {
	unlimited *unlimitedLimiter
	strategy  Strategy

	// maxCount is the number of tokens granted per interval. It is atomic so
	// GetLimit/SetLimit stay lock-free.
	maxCount atomic.Uint32
	interval time.Duration

	// mu guards the fixed-window state (count, next, interval) for the None
	// strategy. The critical section is only a few field operations.
	mu    sync.Mutex
	count uint32    // tokens remaining in the current window
	next  time.Time // end of the current window (when count refills)

	ctx context.Context
	// done is closed by Stop to release any waiter blocked in Take.
	done     chan struct{}
	stopOnce sync.Once

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

// Take one token from the bucket
func (limiter *Limiter) Take() {
	if limiter.unlimited != nil {
		if finite := limiter.unlimited.bucket(); finite != nil {
			finite.Take()
		}

		return
	}

	if limiter.strategy == LeakyBucket {
		_ = limiter.leakyBucketLimiter.Wait(context.TODO())
		return
	}

	for {
		limiter.mu.Lock()
		now := time.Now()
		switch {
		case limiter.next.IsZero():
			// first take in this window; the initial budget is already set
			limiter.next = now.Add(limiter.interval)
		case !now.Before(limiter.next):
			// one or more windows elapsed, refill to the cap. Advance from the
			// previous boundary to avoid drift, snapping forward if the limiter
			// sat idle for longer than a full window.
			limiter.count = limiter.maxCount.Load()
			limiter.next = limiter.next.Add(limiter.interval)
			if now.After(limiter.next) {
				limiter.next = now.Add(limiter.interval)
			}
		}
		if limiter.count > 0 {
			limiter.count--
			limiter.mu.Unlock()
			return
		}
		wait := time.Until(limiter.next)
		limiter.mu.Unlock()

		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		var ctxDone <-chan struct{}
		if limiter.ctx != nil {
			ctxDone = limiter.ctx.Done()
		}
		select {
		case <-timer.C:
		case <-ctxDone:
			timer.Stop()
			return
		case <-limiter.done:
			timer.Stop()
			return
		}
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

	if limiter.strategy == LeakyBucket {
		return limiter.leakyBucketLimiter.Tokens() > 0
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	// a rolled-over window will refill on the next take
	if !limiter.next.IsZero() && !time.Now().Before(limiter.next) {
		return true
	}
	return limiter.count > 0
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
	if limiter.strategy == LeakyBucket {
		limiter.leakyBucketLimiter.SetBurst(int(max))
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

	if limiter.strategy == LeakyBucket {
		limiter.interval = d
		limiter.leakyBucketLimiter.SetLimit(rate.Every(d))
		return
	}
	limiter.mu.Lock()
	limiter.interval = d
	limiter.next = time.Now().Add(d)
	limiter.mu.Unlock()
}

// Stop the rate limiter releasing any waiter blocked in Take
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

	if limiter.strategy == LeakyBucket {
		return
	}
	limiter.stopOnce.Do(func() {
		if limiter.done != nil {
			close(limiter.done)
		}
	})
}

// New creates a new limiter instance with the tokens amount and the interval
func New(ctx context.Context, max uint, duration time.Duration) *Limiter {
	limiter := &Limiter{
		strategy: None,
		interval: duration,
		ctx:      ctx,
		done:     make(chan struct{}),
	}

	limiter.maxCount.Store(uint32(max))
	limiter.count = uint32(max)
	limiter.next = time.Now().Add(duration)
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
	limiter := &Limiter{
		strategy:           LeakyBucket,
		leakyBucketLimiter: rate.NewLimiter(rate.Every(duration), int(max)),
	}

	limiter.maxCount.Store(uint32(max))
	limiter.interval = duration

	return limiter
}
