package check

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/BilalWarraich/pingr/ratelimit"
	"golang.org/x/sync/errgroup"
)

type Result struct {
	URL      string        `json:"url"`
	Status   int           `json:"status"`
	Up       bool          `json:"up"`
	Latency  time.Duration `json:"latency_ns"`
	Attempts int           `json:"attempts"`
	Err      string        `json:"error,omitempty"`
	At       time.Time     `json:"checked_at"`
	// only set for https urls
	CertExpiry *time.Time `json:"cert_expires_at,omitempty"`
}

// CertDaysLeft returns whole days until the cert expires, ok=false if there's no cert info.
func (r Result) CertDaysLeft() (days int, ok bool) {
	if r.CertExpiry == nil {
		return 0, false
	}
	return int(time.Until(*r.CertExpiry).Hours() / 24), true
}

type Checker struct {
	Client      *http.Client
	Limiter     *ratelimit.Bucket // optional
	Concurrency int
	Retries     int
}

// CheckAll checks every url with at most c.Concurrency in flight.
// Results come back in the same order as urls.
func (c *Checker) CheckAll(ctx context.Context, urls []string) ([]Result, error) {
	results := make([]Result, len(urls))

	g, ctx := errgroup.WithContext(ctx)
	if c.Concurrency > 0 {
		g.SetLimit(c.Concurrency)
	}

	for i, u := range urls {
		// each goroutine writes to its own index so no locking needed
		g.Go(func() error {
			results[i] = c.Check(ctx, u)
			// a down site isn't an error for the group, only ctx cancellation is
			return ctx.Err()
		})
	}

	err := g.Wait()
	return results, err
}

// Stream is like CheckAll but sends results on a channel as soon as they're ready.
// The channel is closed once everything is done.
func (c *Checker) Stream(ctx context.Context, urls []string) <-chan Result {
	out := make(chan Result)
	jobs := make(chan string)

	n := c.Concurrency
	if n <= 0 {
		n = 1
	}

	go func() {
		defer close(jobs)
		for _, u := range urls {
			select {
			case jobs <- u:
			case <-ctx.Done():
				return
			}
		}
	}()

	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for u := range jobs {
				select {
				case out <- c.Check(ctx, u):
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		for i := 0; i < n; i++ {
			<-done
		}
		close(out)
	}()

	return out
}

func (c *Checker) Check(ctx context.Context, url string) Result {
	r := Result{URL: url}
	backoff := 200 * time.Millisecond

	for attempt := 0; attempt <= c.Retries; attempt++ {
		r.Attempts = attempt + 1
		if c.Limiter != nil {
			if err := c.Limiter.Wait(ctx); err != nil {
				r.Err = err.Error()
				break
			}
		}

		status, lat, certExp, err := c.do(ctx, url)
		r.Status, r.Latency, r.At, r.CertExpiry = status, lat, time.Now(), certExp
		if err == nil && status < 500 {
			r.Up = status < 400
			r.Err = ""
			if !r.Up {
				r.Err = fmt.Sprintf("got %d", status)
			}
			// 4xx won't get better by retrying
			return r
		}
		if err != nil {
			r.Err = err.Error()
		} else {
			r.Err = fmt.Sprintf("got %d", status)
		}

		if attempt < c.Retries {
			select {
			case <-time.After(backoff):
				backoff *= 2
			case <-ctx.Done():
				r.Err = ctx.Err().Error()
				return r
			}
		}
	}
	return r
}

func (c *Checker) do(ctx context.Context, url string) (int, time.Duration, *time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, nil, err
	}
	req.Header.Set("User-Agent", "pingr/0.1")

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	start := time.Now()
	resp, err := client.Do(req)
	lat := time.Since(start)
	if err != nil {
		return 0, lat, nil, err
	}
	resp.Body.Close()

	// PeerCertificates[0] is the leaf cert, the one for the site itself.
	// note: after redirects this is the cert of the final url
	var certExp *time.Time
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		exp := resp.TLS.PeerCertificates[0].NotAfter
		certExp = &exp
	}
	return resp.StatusCode, lat, certExp, nil
}
