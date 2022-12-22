package ratelimit

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/exp/maps"
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
type MultiLimiter struct {
	limiters map[string]*Limiter
	ctx      context.Context
}

// Adds new bucket with key
func (m *MultiLimiter) Add(opts *Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	_, ok := m.limiters[opts.Key]
	if ok {
		return fmt.Errorf("key already exists")
	}
	var rlimiter *Limiter
	if opts.IsUnlimited {
		rlimiter = NewUnlimited(m.ctx)
	} else {
		rlimiter = New(m.ctx, opts.MaxCount, opts.Duration)
	}
	m.limiters[opts.Key] = rlimiter
	return nil
}

// GetLimit returns current ratelimit of given key
func (m *MultiLimiter) GetLimit(key string) (uint, error) {
	limiter, ok := m.limiters[key]
	if !ok || limiter == nil {
		return 0, fmt.Errorf("key doesnot exist")
	}
	return limiter.GetLimit(), nil
}

// Take one token from bucket returns error if key not present
func (m *MultiLimiter) Take(key string) error {
	limiter, ok := m.limiters[key]
	if !ok || limiter == nil {
		return fmt.Errorf("key doesnot exist")
	}
	limiter.Take()
	return nil
}

// Stop internal limiters with defined keys or all if no key is provided
func (m *MultiLimiter) Stop(keys ...string) {
	if len(keys) > 0 {
		m.stopWithKeys(keys...)
	} else {
		m.stopWithKeys(maps.Keys(m.limiters)...)
	}
}

// stopWithKeys stops the internal limiters matching keys
func (m *MultiLimiter) stopWithKeys(keys ...string) {
	for _, key := range keys {
		if limiter, ok := m.limiters[key]; ok {
			limiter.Stop()
		}
	}
}

// SleepandReset stops timer removes all tokens and resets with new limit (used for Adaptive Ratelimiting)
func (m *MultiLimiter) SleepandReset(SleepTime time.Duration, opts *Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	limiter, ok := m.limiters[opts.Key]
	if !ok || limiter == nil {
		return fmt.Errorf("key doesnot exist")
	}
	limiter.SleepandReset(SleepTime, opts.MaxCount, opts.Duration)
	return nil
}

// NewMultiLimiter : Limits
func NewMultiLimiter(ctx context.Context, opts *Options) (*MultiLimiter, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	multilimiter := &MultiLimiter{
		ctx:      ctx,
		limiters: map[string]*Limiter{},
	}
	return multilimiter, multilimiter.Add(opts)
}
