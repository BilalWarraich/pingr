package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BilalWarraich/pingr/check"
	"gopkg.in/yaml.v3"
)

// config file format, see pingr.example.yaml
type config struct {
	URLs []struct {
		URL            string        `yaml:"url"`
		ExpectedStatus int           `yaml:"expected_status"`
		BodyContains   string        `yaml:"body_contains"`
		Timeout        time.Duration `yaml:"timeout"`
	} `yaml:"urls"`
}

func loadConfig(path string) ([]string, map[string]check.Rule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var cfg config
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // typos like "expect_status" should be an error, not silently ignored
	if err := dec.Decode(&cfg); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}

	var urls []string
	rules := make(map[string]check.Rule)
	for i, u := range cfg.URLs {
		url := strings.TrimSpace(u.URL)
		if url == "" {
			return nil, nil, fmt.Errorf("%s: urls[%d] has no url", path, i)
		}
		if !strings.Contains(url, "://") {
			url = "https://" + url
		}
		urls = append(urls, url)
		rules[url] = check.Rule{
			ExpectStatus: u.ExpectedStatus,
			BodyContains: u.BodyContains,
			Timeout:      u.Timeout,
		}
	}
	return urls, rules, nil
}
