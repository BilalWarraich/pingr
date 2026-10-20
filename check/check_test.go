package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckAll(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()

	c := &Checker{Concurrency: 2}
	res, err := c.CheckAll(context.Background(), []string{ok.URL, notFound.URL, "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}

	if !res[0].Up {
		t.Errorf("expected %s up: %+v", ok.URL, res[0])
	}
	if res[1].Up || res[1].Status != 404 {
		t.Errorf("expected 404 down: %+v", res[1])
	}
	if res[2].Up || res[2].Err == "" {
		t.Errorf("expected conn error: %+v", res[2])
	}
}

func TestRetriesOn5xx(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()

	c := &Checker{Retries: 3}
	r := c.Check(context.Background(), srv.URL)
	if !r.Up || r.Attempts != 3 {
		t.Fatalf("got %+v", r)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var inflight, maxSeen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}))
	defer srv.Close()

	urls := make([]string, 20)
	for i := range urls {
		urls[i] = srv.URL
	}

	c := &Checker{Concurrency: 3}
	c.CheckAll(context.Background(), urls)
	if maxSeen.Load() > 3 {
		t.Fatalf("saw %d concurrent requests, limit was 3", maxSeen.Load())
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := &Checker{Concurrency: 4}
	count := 0
	for r := range c.Stream(context.Background(), []string{srv.URL, srv.URL, srv.URL, srv.URL, srv.URL}) {
		if !r.Up {
			t.Errorf("not up: %+v", r)
		}
		count++
	}
	if count != 5 {
		t.Fatalf("got %d results", count)
	}
}
