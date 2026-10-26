// Package alert sends a message when a url changes state (up -> down or back).
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/BilalWarraich/pingr/check"
)

type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// Slack posts to an incoming webhook url.
type Slack struct {
	WebhookURL string
	Client     *http.Client
}

func (s *Slack) Notify(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}
	return nil
}

// Tracker remembers the last state of each url so we only alert on changes,
// not every time a down site is checked again.
type Tracker struct {
	mu   sync.Mutex
	last map[string]bool // url -> up
}

func NewTracker() *Tracker {
	return &Tracker{last: make(map[string]bool)}
}

// Changed returns a message if r is a state change worth alerting on.
// The first result for a url only alerts if it's down - no point getting
// "x is up" for every url every time pingr restarts.
func (t *Tracker) Changed(r check.Result) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	prev, seen := t.last[r.URL]
	t.last[r.URL] = r.Up

	switch {
	case !seen && !r.Up, seen && prev && !r.Up:
		return fmt.Sprintf(":red_circle: %s is DOWN (%s)", r.URL, r.Err), true
	case seen && !prev && r.Up:
		return fmt.Sprintf(":large_green_circle: %s is back up (%v)", r.URL, r.Latency.Round(time.Millisecond)), true
	}
	return "", false
}
