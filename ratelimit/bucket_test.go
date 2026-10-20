package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestBurstThenEmpty(t *testing.T) {
	b := New(1, 3)
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if b.Allow() {
		t.Fatal("bucket should be empty")
	}
}

func TestRefill(t *testing.T) {
	now := time.Now()
	b := New(10, 1)
	b.nowFunc = func() time.Time { return now }
	b.last = now

	b.Allow()
	if b.Allow() {
		t.Fatal("should be empty")
	}
	now = now.Add(100 * time.Millisecond) // 10/s -> 1 token
	if !b.Allow() {
		t.Fatal("should have refilled one token")
	}
}

func TestWaitRespectsContext(t *testing.T) {
	b := New(0.1, 1) // super slow
	b.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := b.Wait(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestWaitRate(t *testing.T) {
	b := New(50, 1)
	start := time.Now()
	for i := 0; i < 6; i++ {
		b.Wait(context.Background())
	}
	// 1 from burst + 5 at 50/s = ~100ms
	if el := time.Since(start); el < 80*time.Millisecond {
		t.Fatalf("too fast: %v", el)
	}
}
