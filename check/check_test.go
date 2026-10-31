package check

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCertExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// srv.Client() trusts the test server's self-signed cert
	c := &Checker{Client: srv.Client()}
	r := c.Check(context.Background(), srv.URL)
	if !r.Up {
		t.Fatalf("not up: %+v", r)
	}
	days, ok := r.CertDaysLeft()
	if !ok {
		t.Fatal("expected cert info for https url")
	}
	want := int(time.Until(srv.Certificate().NotAfter).Hours() / 24)
	if days != want {
		t.Fatalf("days left = %d, want %d", days, want)
	}
}

func TestNoCertForPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	r := (&Checker{}).Check(context.Background(), srv.URL)
	if _, ok := r.CertDaysLeft(); ok {
		t.Fatal("plain http shouldn't have cert info")
	}
}

func TestRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/maintenance":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/broken":
			fmt.Fprint(w, `{"status":"degraded"}`)
		case "/slow":
			time.Sleep(200 * time.Millisecond)
		}
	}))
	defer srv.Close()

	cases := []struct {
		name    string
		path    string
		rule    Rule
		wantUp  bool
		wantErr string
	}{
		{"no rule", "/health", Rule{}, true, ""},
		{"body matches", "/health", Rule{BodyContains: `"ok"`}, true, ""},
		{"body doesn't match", "/broken", Rule{BodyContains: `"ok"`}, false, "body doesn't contain"},
		{"expected 503 is up", "/maintenance", Rule{ExpectStatus: 503}, true, ""},
		{"wrong status", "/health", Rule{ExpectStatus: 201}, false, "expected 201"},
		{"timeout", "/slow", Rule{Timeout: 50 * time.Millisecond}, false, "deadline exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := srv.URL + tc.path
			c := &Checker{Rules: map[string]Rule{url: tc.rule}}
			r := c.Check(context.Background(), url)
			if r.Up != tc.wantUp {
				t.Fatalf("up=%v want %v (%+v)", r.Up, tc.wantUp, r)
			}
			if tc.wantErr != "" && !strings.Contains(r.Err, tc.wantErr) {
				t.Fatalf("err %q should contain %q", r.Err, tc.wantErr)
			}
			if r.Attempts != 1 {
				t.Fatalf("shouldn't retry here, attempts=%d", r.Attempts)
			}
		})
	}
}
