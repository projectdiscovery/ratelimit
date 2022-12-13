package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

/*
Note:
This is somewhat modified version of TokenBucket
Here we consider buffer channel as a bucket
*/

// MultiLimiter allows burst of request during defined duration for each key
type MultiLimiter struct {
	ticker *time.Ticker
	tokens sync.Map // map of buffered channels map[string](chan struct{})
	ctx    context.Context
}

func (m *MultiLimiter) run() {
	for {
		select {
		case <-m.ctx.Done():
			m.ticker.Stop()
			return

		case <-m.ticker.C:
			// Iterate and fill buffers to their capacity on every tick
			m.tokens.Range(func(key, value any) bool {
				tokenChan := value.(chan struct{})
				if len(tokenChan) == cap(tokenChan) {
					// no need to fill buffer/bucket
					return true
				} else {
					for i := 0; i < cap(tokenChan)-len(tokenChan); i++ {
						// fill bucket/buffer with tokens
						tokenChan <- struct{}{}
					}
				}
				// if it returns false range is stopped
				return true
			})
		}
	}
}

// Adds new bucket with key and given tokenrate returns error if it already exists1
func (m *MultiLimiter) Add(key string, tokensPerMinute uint) error {
	_, ok := m.tokens.Load(key)
	if ok {
		return fmt.Errorf("key already exists")
	}
	// create a buffered channel of size `tokenPerMinute`
	tokenChan := make(chan struct{}, tokensPerMinute)
	for i := 0; i < int(tokensPerMinute); i++ {
		// fill bucket/buffer with tokens
		tokenChan <- struct{}{}
	}
	m.tokens.Store(key, tokenChan)
	return nil
}

// Take one token from bucket / buffer returns error if key not present
func (m *MultiLimiter) Take(key string) error {
	tokenValue, ok := m.tokens.Load(key)
	if !ok {
		return fmt.Errorf("key doesnot exist")
	}
	tokenChan := tokenValue.(chan struct{})
	<-tokenChan

	return nil
}

// NewMultiLimiter : Limits
func NewMultiLimiter(ctx context.Context) *MultiLimiter {
	multilimiter := &MultiLimiter{
		ticker: time.NewTicker(time.Minute), // different implementation than ratelimit
		ctx:    ctx,
		tokens: sync.Map{},
	}

	go multilimiter.run()

	return multilimiter
}
