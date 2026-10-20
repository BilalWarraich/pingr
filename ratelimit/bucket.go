// Package ratelimit is a tiny token bucket. Yes, golang.org/x/time/rate exists,
// I wanted to write one myself.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Bucket struct {
	mu      sync.Mutex
	tokens  float64
	max     float64
	rate    float64 // tokens per second
	last    time.Time
	nowFunc func() time.Time
}

// New returns a bucket that allows `rate` events per second with bursts up to `burst`.
func New(rate float64, burst int) *Bucket {
	return &Bucket{
		tokens:  float64(burst),
		max:     float64(burst),
		rate:    rate,
		last:    time.Now(),
		nowFunc: time.Now,
	}
}

func (b *Bucket) refill() {
	now := b.nowFunc()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
}

// Allow takes a token if one is available, without waiting.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available or ctx is cancelled.
func (b *Bucket) Wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		b.refill()
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		// how long until we have one full token
		wait := time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()

		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}
