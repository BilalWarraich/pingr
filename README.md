# pingr

[![ci](https://github.com/BilalWarraich/pingr/actions/workflows/ci.yml/badge.svg)](https://github.com/BilalWarraich/pingr/actions/workflows/ci.yml)

Checks a list of urls concurrently and tells you which ones are down.
Started as a weekend thing to check my own side projects, ended up being a nice
excuse to try out a few concurrency patterns in Go.

## What's in here

- `check.CheckAll` - bounded concurrency with `errgroup.SetLimit`, results keep input order
- `check.Stream` - classic worker pool / fan-out fan-in with channels, results come out as soon as they're ready
- `ratelimit` - small token bucket (I know `x/time/rate` exists, wanted to write one)
- retries with exponential backoff on network errors and 5xx (not on 4xx)
- `-serve` mode runs checks on an interval and exposes prometheus metrics
- everything is ctx-aware so ctrl+c actually stops things

## Usage

```
go build -o pingr .
./pingr -f urls.txt
```

```
STATUS  CODE  LATENCY  TRIES  URL                                         ERROR
UP      200   156ms    1      https://go.dev
UP      200   212ms    1      https://github.com
DOWN    503   402ms    3      https://httpbin.org/status/503              got 503
UP      200   1.31s    1      https://httpbin.org/delay/1
DOWN    -     3ms      3      https://this-domain-does-not-exist-123.com  dial tcp: lookup ...
```

Exit code is 1 if anything is down, so it works in a cron job / CI step.

Flags:

```
-f         file with urls (default urls.txt)
-c         max concurrent requests (10)
-rps       max requests per second, 0 = unlimited (20)
-timeout   per request timeout (5s)
-retries   retries on errors/5xx (2)
-json      print json lines as results come in
-serve     run forever, expose /metrics and /status on this addr
-interval  check interval in serve mode (30s)
```

### Serve mode

```
./pingr -serve :9100 -interval 15s
curl localhost:9100/metrics | grep pingr_
curl localhost:9100/status
```

Metrics: `pingr_up{url}`, `pingr_latency_seconds{url}`, `pingr_checks_total{url,result}`

Or with docker:

```
docker build -t pingr .
docker run -p 9100:9100 pingr
```

## Free uptime checks with GitHub Actions

`.github/workflows/uptime.yml` runs pingr every 30 minutes against `uptime-urls.txt`.
If anything is down the run fails (and GitHub emails you), and the results table shows up
in the run summary. Free for public repos, nothing to host.

Note: github pauses scheduled workflows after 60 days with no commits in the repo,
you can re-enable it from the Actions tab.

## Tests

```
go test -race ./...
```

## Ideas / todo

- alerting (slack webhook?) when something flips from up to down
- per-url config (expected status, body contains, custom interval)
- TLS cert expiry check
