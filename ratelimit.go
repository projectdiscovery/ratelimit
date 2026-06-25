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
	strategy Strategy

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

// Take one token from the bucket
func (limiter *Limiter) Take() {
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

// SetLimit sets the current rate limit per given duration
func (limiter *Limiter) SetLimit(max uint) {
	limiter.maxCount.Store(uint32(max))
	if limiter.strategy == LeakyBucket {
		limiter.leakyBucketLimiter.SetBurst(int(max))
	}
}

// SetDuration sets the current rate limit duration
func (limiter *Limiter) SetDuration(d time.Duration) {
	if limiter.strategy == LeakyBucket {
		limiter.interval = d
		limiter.leakyBucketLimiter.SetLimit(rate.Every(d))
		return
	}
	limiter.mu.Lock()
	limiter.interval = d
	// recompute the window boundary against the new duration on the next take
	limiter.next = time.Time{}
	limiter.mu.Unlock()
}

// Stop the rate limiter releasing any waiter blocked in Take
func (limiter *Limiter) Stop() {
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
	return limiter
}

// NewUnlimited create a bucket with approximated unlimited tokens
func NewUnlimited(ctx context.Context) *Limiter {
	limiter := &Limiter{
		strategy: None,
		interval: time.Millisecond,
		ctx:      ctx,
		done:     make(chan struct{}),
	}
	limiter.maxCount.Store(math.MaxUint32)
	limiter.count = math.MaxUint32
	return limiter
}

// NewLeakyBucket create a bucket with a smooth (leaky bucket) token rate
func NewLeakyBucket(ctx context.Context, max uint, duration time.Duration) *Limiter {
	limiter := &Limiter{
		strategy:           LeakyBucket,
		leakyBucketLimiter: rate.NewLimiter(rate.Every(duration), int(max)),
	}
	limiter.maxCount.Store(uint32(max))
	limiter.interval = duration
	return limiter
}
