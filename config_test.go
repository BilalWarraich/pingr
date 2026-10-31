package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pingr.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfig(t *testing.T) {
	p := writeTemp(t, `
urls:
  - url: go.dev
  - url: https://api.example.com/health
    expected_status: 200
    body_contains: '"ok"'
    timeout: 2s
`)
	urls, rules, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 || urls[0] != "https://go.dev" {
		t.Fatalf("urls = %v", urls)
	}
	r := rules["https://api.example.com/health"]
	if r.ExpectStatus != 200 || r.BodyContains != `"ok"` || r.Timeout != 2*time.Second {
		t.Fatalf("rule = %+v", r)
	}
}

func TestLoadConfigTypo(t *testing.T) {
	p := writeTemp(t, `
urls:
  - url: go.dev
    expect_status: 200
`)
	_, _, err := loadConfig(p)
	if err == nil || !strings.Contains(err.Error(), "expect_status") {
		t.Fatalf("expected an error about the unknown field, got %v", err)
	}
}
