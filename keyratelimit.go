package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Options of MultiLimiter
type Options struct {
	Key         string // Unique Identifier
	IsUnlimited bool
	MaxCount    uint
	Duration    time.Duration
}

// Validate given MultiLimiter Options
func (o *Options) Validate() error {
	if !o.IsUnlimited {
		if o.Key == "" {
			return fmt.Errorf("empty keys not allowed")
		}
		if o.MaxCount == 0 {
			return fmt.Errorf("maxcount cannot be zero")
		}
		if o.Duration == 0 {
			return fmt.Errorf("time duration not set")
		}
	}
	return nil
}

// MultiLimiter is wrapper around Limiter than can limit based on a key
// If Invalid keys or options are given then Multilimiter just skips it (similar to sync.Map)
type MultiLimiter struct {
	limiters sync.Map
	ctx      context.Context
}

// Adds new bucket with key
func (m *MultiLimiter) Add(opts *Options) bool {
	var rlimiter *Limiter
	if opts.IsUnlimited {
		rlimiter = NewUnlimited(m.ctx)
	} else {
		rlimiter = New(m.ctx, opts.MaxCount, opts.Duration)
	}

	_, ok := m.limiters.LoadOrStore(opts.Key, rlimiter)
	return ok
}

// GetLimit returns current ratelimit of given key
func (m *MultiLimiter) GetLimit(key string) uint {
	if limiter, ok := m.limiters.Load(key); ok {
		rl := (limiter).(*Limiter)
		return rl.GetLimit()
	}
	return 0
}

// Take one token from bucket returns error if key not present
func (m *MultiLimiter) Take(key string) bool {
	limiter, ok := m.limiters.Load(key)
	rl := (limiter).(*Limiter)
	if ok {
		rl.Take()
		return true
	}
	return false
}

// TakeNCreate creates multilimiter if not exists
func (m *MultiLimiter) TakeNCreate(opts *Options) {
	var rlimiter *Limiter
	if opts.IsUnlimited {
		rlimiter = NewUnlimited(m.ctx)
	} else {
		rlimiter = New(m.ctx, opts.MaxCount, opts.Duration)
	}

	limiter, _ := m.limiters.LoadOrStore(opts.Key, rlimiter)
	rl := (limiter).(*Limiter)
	rl.Take()
}

// Stop internal limiters with defined keys or all if no key is provided
func (m *MultiLimiter) Stop(keys ...string) {
	if len(keys) == 0 {
		m.limiters.Range(func(key, value any) bool {
			keys = append(keys, key.(string))
			return true
		})
	}
	for _, v := range keys {
		m.limiters.Delete(v)
	}
}

// SleepandReset stops timer removes all tokens and resets with new limit (used for Adaptive Ratelimiting)
func (m *MultiLimiter) SleepandReset(SleepTime time.Duration, opts *Options) bool {
	if err := opts.Validate(); err != nil {
		return false
	}
	limiter, ok := m.limiters.Load(opts.Key)
	if !ok || limiter == nil {
		return false
	}
	rl := limiter.(*Limiter)
	rl.SleepandReset(SleepTime, opts.MaxCount, opts.Duration)
	return true
}

// NewMultiLimiter : Limits
func NewMultiLimiter(ctx context.Context, opts *Options) *MultiLimiter {
	if err := opts.Validate(); err != nil {
		return nil
	}
	multilimiter := &MultiLimiter{
		ctx:      ctx,
		limiters: sync.Map{},
	}
	return multilimiter
}
