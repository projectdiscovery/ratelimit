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
type MultiLimiter struct {
	limiters sync.Map // map of limiters
	ctx      context.Context
}

// Adds new bucket with key
func (m *MultiLimiter) Add(opts *Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	var rlimiter *Limiter
	if opts.IsUnlimited {
		rlimiter = NewUnlimited(m.ctx)
	} else {
		rlimiter = New(m.ctx, opts.MaxCount, opts.Duration)
	}
	// ok if true if key already exists
	_, ok := m.limiters.LoadOrStore(opts.Key, rlimiter)
	if ok {
		return fmt.Errorf("key already exists")
	}
	return nil
}

// GetLimit returns current ratelimit of given key
func (m *MultiLimiter) GetLimit(key string) (uint, error) {
	limiter, err := m.get(key)
	if err != nil {
		return 0, err
	}
	return limiter.GetLimit(), nil
}

// Take one token from bucket returns error if key not present
func (m *MultiLimiter) Take(key string) error {
	limiter, err := m.get(key)
	if err != nil {
		return err
	}
	limiter.Take()
	return nil
}

// AddAndTake adds key if not present and then takes token from bucket
func (m *MultiLimiter) AddAndTake(opts *Options) {
	if limiter, err := m.get(opts.Key); err == nil {
		limiter.Take()
		return
	}
	m.Add(opts)
	m.Take(opts.Key)
}

// Stop internal limiters with defined keys or all if no key is provided
func (m *MultiLimiter) Stop(keys ...string) {
	if len(keys) == 0 {
		m.limiters.Range(func(key, value any) bool {
			value.(*Limiter).Stop()
			return true
		})
		return
	}
	for _, v := range keys {
		if limiter, err := m.get(v); err == nil {
			limiter.Stop()
		}
	}
}

// get returns *Limiter instance
func (m *MultiLimiter) get(key string) (*Limiter, error) {
	val, _ := m.limiters.Load(key)
	if val == nil {
		return nil, fmt.Errorf("multilimiter: key does not exist")
	}
	return val.(*Limiter), nil
}

// NewMultiLimiter : Limits
func NewMultiLimiter(ctx context.Context, opts *Options) (*MultiLimiter, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	multilimiter := &MultiLimiter{
		ctx:      ctx,
		limiters: sync.Map{},
	}
	return multilimiter, multilimiter.Add(opts)
}
