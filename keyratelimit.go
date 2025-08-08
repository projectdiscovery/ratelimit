package ratelimit

import (
	"context"
	"sync"
	"time"

	errorutil "github.com/projectdiscovery/utils/errors"
)

var (
	// ErrKeyAlreadyExists is returned when attempting to add a limiter with an existing key.
	ErrKeyAlreadyExists = errorutil.NewWithTag("MultiLimiter", "key already exists")
	// ErrKeyMissing is returned when a limiter key is not present.
	ErrKeyMissing = errorutil.NewWithTag("MultiLimiter", "key does not exist")
)

// Options describes the configuration for a limiter bucket tracked by MultiLimiter.
type Options struct {
	// Key uniquely identifies the limiter within a MultiLimiter.
	// It MUST be non-empty.
	Key string
	// IsUnlimited creates an unlimited limiter for the given key when true.
	IsUnlimited bool
	// MaxCount is the number of tokens per Duration (required when IsUnlimited is false).
	MaxCount uint
	// Duration is the refill window (required when IsUnlimited is false).
	Duration time.Duration
}

// Validate checks Options for correctness.
func (o *Options) Validate() error {
	// Key must always be non-empty because it is used as the map key.
	if o.Key == "" {
		return errorutil.NewWithTag("MultiLimiter", "empty keys not allowed")
	}
	if o.IsUnlimited {
		return nil
	}
	if o.MaxCount == 0 {
		return errorutil.NewWithTag("MultiLimiter", "maxcount cannot be zero")
	}
	if o.Duration <= 0 {
		return errorutil.NewWithTag("MultiLimiter", "time duration must be > 0")
	}
	return nil
}

// MultiLimiter wraps Limiter instances keyed by string.
type MultiLimiter struct {
	limiters sync.Map       // map[string]*Limiter
	ctx      context.Context
}

// Add registers a new limiter at opts.Key.
// Returns ErrKeyAlreadyExists if the key is already present.
func (m *MultiLimiter) Add(opts *Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	var limiter *Limiter
	if opts.IsUnlimited {
		limiter = NewUnlimited(m.ctx)
	} else {
		limiter = New(m.ctx, opts.MaxCount, opts.Duration)
	}
	// ok is true if key already exists
	_, ok := m.limiters.LoadOrStore(opts.Key, limiter)
	if ok {
		return ErrKeyAlreadyExists.Msgf("key: %v", opts.Key)
	}
	return nil
}

// GetLimit returns the current rate limit (max tokens) for a given key.
func (m *MultiLimiter) GetLimit(key string) (uint, error) {
	limiter, err := m.get(key)
	if err != nil {
		return 0, err
	}
	return limiter.GetLimit(), nil
}

// Take consumes one token from the bucket for the given key.
// Returns ErrKeyMissing if the key is not present.
func (m *MultiLimiter) Take(key string) error {
	limiter, err := m.get(key)
	if err != nil {
		return err
	}
	limiter.Take()
	return nil
}

// CanTake reports whether the limiter for key currently has a token available.
// Returns false if the key is missing or the limiter cannot take.
func (m *MultiLimiter) CanTake(key string) bool {
	limiter, err := m.get(key)
	if err != nil {
		return false
	}
	return limiter.CanTake()
}

// CanTakeErr is like CanTake but returns a descriptive error when the key is missing.
func (m *MultiLimiter) CanTakeErr(key string) (bool, error) {
	limiter, err := m.get(key)
	if err != nil {
		return false, err
	}
	return limiter.CanTake(), nil
}

// AddAndTake adds the key if not present and then takes a token from the bucket.
// Legacy behavior: ignores errors. Prefer AddAndTakeErr for new code.
func (m *MultiLimiter) AddAndTake(opts *Options) {
	if limiter, err := m.get(opts.Key); err == nil {
		limiter.Take()
		return
	}
	_ = m.Add(opts)
	_ = m.Take(opts.Key)
}

// AddAndTakeErr is the same as AddAndTake but returns an error instead of swallowing it.
func (m *MultiLimiter) AddAndTakeErr(opts *Options) error {
	if limiter, err := m.get(opts.Key); err == nil {
		limiter.Take()
		return nil
	}
	if err := m.Add(opts); err != nil {
		return err
	}
	return m.Take(opts.Key)
}

// Stop gracefully stops internal limiters for the provided keys.
// If no keys are provided, it stops all limiters but does not remove them from the map.
func (m *MultiLimiter) Stop(keys ...string) {
	if len(keys) == 0 {
		m.limiters.Range(func(key, value any) bool {
			if limiter, ok := value.(*Limiter); ok {
				limiter.Stop()
			}
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

// Delete removes limiters for the given keys after calling Stop on them.
// If no keys provided, it deletes nothing. Use Stop() first to stop all if needed.
func (m *MultiLimiter) Delete(keys ...string) {
	for _, k := range keys {
		if val, ok := m.limiters.LoadAndDelete(k); ok {
			if limiter, ok := val.(*Limiter); ok {
				limiter.Stop()
			}
		}
	}
}

// Exists reports whether a limiter for key exists.
func (m *MultiLimiter) Exists(key string) bool {
	_, ok := m.limiters.Load(key)
	return ok
}

// Keys returns a snapshot of all keys currently registered.
func (m *MultiLimiter) Keys() []string {
	keys := make([]string, 0, 8)
	m.limiters.Range(func(key, _ any) bool {
		if ks, ok := key.(string); ok {
			keys = append(keys, ks)
		}
		return true
	})
	return keys
}

// get returns the *Limiter instance for a key or an error.
func (m *MultiLimiter) get(key string) (*Limiter, error) {
	val, ok := m.limiters.Load(key)
	if !ok || val == nil {
		return nil, ErrKeyMissing.Msgf("key: %v", key)
	}
	if limiter, ok := val.(*Limiter); ok {
		return limiter, nil
	}
	return nil, errorutil.NewWithTag("MultiLimiter", "type assertion of Limiter failed in MultiLimiter")
}

// NewMultiLimiterEmpty creates a MultiLimiter without adding an initial limiter.
func NewMultiLimiterEmpty(ctx context.Context) *MultiLimiter {
	return &MultiLimiter{
		ctx:      ctx,
		limiters: sync.Map{},
	}
}

// NewMultiLimiter creates a MultiLimiter and adds the initial limiter defined by opts.
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
