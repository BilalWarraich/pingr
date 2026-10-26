package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BilalWarraich/pingr/check"
)

func TestTrackerOnlyAlertsOnChanges(t *testing.T) {
	tr := NewTracker()
	up := check.Result{URL: "a", Up: true}
	down := check.Result{URL: "a", Up: false, Err: "got 503"}

	steps := []struct {
		r     check.Result
		alert bool
	}{
		{up, false},   // first time seen and up: quiet
		{up, false},   // still up
		{down, true},  // went down
		{down, false}, // still down, don't spam
		{down, false},
		{up, true}, // recovered
		{up, false},
	}
	for i, s := range steps {
		msg, got := tr.Changed(s.r)
		if got != s.alert {
			t.Fatalf("step %d: alert=%v want %v (msg %q)", i, got, s.alert, msg)
		}
	}
}

func TestTrackerAlertsIfDownOnFirstCheck(t *testing.T) {
	tr := NewTracker()
	msg, ok := tr.Changed(check.Result{URL: "b", Up: false, Err: "connection refused"})
	if !ok || !strings.Contains(msg, "DOWN") {
		t.Fatalf("got %q %v", msg, ok)
	}
}

func TestSlackNotify(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()

	s := &Slack{WebhookURL: srv.URL}
	if err := s.Notify(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if got["text"] != "hello" {
		t.Fatalf("slack got %v", got)
	}
}

func TestSlackNotifyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	s := &Slack{WebhookURL: srv.URL}
	if err := s.Notify(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 403")
	}
}
