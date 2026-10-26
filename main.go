package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/BilalWarraich/pingr/alert"
	"github.com/BilalWarraich/pingr/check"
	"github.com/BilalWarraich/pingr/ratelimit"
)

func main() {
	var (
		file        = flag.String("f", "urls.txt", "file with one url per line")
		concurrency = flag.Int("c", 10, "max concurrent requests")
		rps         = flag.Float64("rps", 20, "max requests per second (0 = no limit)")
		timeout     = flag.Duration("timeout", 5*time.Second, "per request timeout")
		retries     = flag.Int("retries", 2, "retries on network errors / 5xx")
		jsonOut     = flag.Bool("json", false, "print results as json")
		serve       = flag.String("serve", "", "run forever and expose /metrics on this addr, e.g. :9100")
		interval    = flag.Duration("interval", 30*time.Second, "how often to check in -serve mode")
	)
	flag.Parse()

	urls, err := readURLs(*file)
	if err != nil {
		log.Fatal(err)
	}
	if len(urls) == 0 {
		log.Fatalf("no urls in %s", *file)
	}

	c := &check.Checker{
		Client:      &http.Client{Timeout: *timeout},
		Concurrency: *concurrency,
		Retries:     *retries,
	}
	if *rps > 0 {
		c.Limiter = ratelimit.New(*rps, int(*rps))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *serve != "" {
		var notifier alert.Notifier
		if hook := os.Getenv("SLACK_WEBHOOK_URL"); hook != "" {
			notifier = &alert.Slack{WebhookURL: hook}
			log.Println("slack alerts on")
		}
		if err := runServer(ctx, *serve, *interval, c, urls, notifier); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *jsonOut {
		// stream json lines as results come in
		enc := json.NewEncoder(os.Stdout)
		for r := range c.Stream(ctx, urls) {
			if err := enc.Encode(r); err != nil {
				// stdout went away (e.g. piped into head), no point continuing
				log.Fatal(err)
			}
		}
		return
	}

	start := time.Now()
	results, err := c.CheckAll(ctx, urls)
	if err != nil {
		log.Printf("interrupted: %v", err)
	}
	printTable(results)
	fmt.Printf("\nchecked %d urls in %v\n", len(urls), time.Since(start).Round(time.Millisecond))

	for _, r := range results {
		if !r.Up {
			os.Exit(1)
		}
	}
}

func readURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "https://" + line
		}
		urls = append(urls, line)
	}
	return urls, sc.Err()
}

func printTable(results []check.Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tCODE\tLATENCY\tTRIES\tURL\tERROR")
	for _, r := range results {
		st := "UP"
		if !r.Up {
			st = "DOWN"
		}
		code := "-"
		if r.Status != 0 {
			code = fmt.Sprint(r.Status)
		}
		fmt.Fprintf(w, "%s\t%s\t%v\t%d\t%s\t%s\n", st, code, r.Latency.Round(time.Millisecond), r.Attempts, r.URL, r.Err)
	}
	w.Flush()
}
