package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/BilalWarraich/pingr/check"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	upGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pingr_up",
		Help: "1 if the last check was successful",
	}, []string{"url"})

	latencyHist = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pingr_latency_seconds",
		Help:    "response time",
		Buckets: prometheus.DefBuckets,
	}, []string{"url"})

	checksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pingr_checks_total",
		Help: "checks done, by result",
	}, []string{"url", "result"})
)

func init() {
	prometheus.MustRegister(upGauge, latencyHist, checksTotal)
}

// status holds the latest result per url for the /status endpoint
type status struct {
	mu      sync.RWMutex
	results map[string]check.Result
}

func (s *status) set(r check.Result) {
	s.mu.Lock()
	s.results[r.URL] = r
	s.mu.Unlock()
}

func (s *status) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.results)
}

func runServer(ctx context.Context, addr string, interval time.Duration, c *check.Checker, urls []string) error {
	st := &status{results: map[string]check.Result{}}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/status", st)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errc := make(chan error, 1)
	go func() {
		log.Printf("serving metrics on %s, checking %d urls every %v", addr, len(urls), interval)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runOnce := func() {
		for r := range c.Stream(ctx, urls) {
			st.set(r)
			record(r)
		}
	}
	runOnce()

loop:
	for {
		select {
		case <-ticker.C:
			runOnce()
		case err := <-errc:
			return err
		case <-ctx.Done():
			break loop
		}
	}

	log.Println("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func record(r check.Result) {
	up := 0.0
	res := "fail"
	if r.Up {
		up = 1
		res = "ok"
	}
	upGauge.WithLabelValues(r.URL).Set(up)
	checksTotal.WithLabelValues(r.URL, res).Inc()
	if r.Status != 0 {
		latencyHist.WithLabelValues(r.URL).Observe(r.Latency.Seconds())
	}
	if !r.Up {
		log.Printf("DOWN %s: %s", r.URL, r.Err)
	}
}
