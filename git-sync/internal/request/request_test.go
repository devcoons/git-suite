package request

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-sync/internal/codes"
)

func TestParseFlags(t *testing.T) {
	opt, _, err := Parse([]string{"--url", "https://github.com/org/repo.git", "--path", "src/repo", "--branch", "main"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if opt.URL == "" || opt.Path == "" || opt.Branch != "main" {
		t.Fatalf("%+v", opt)
	}

	_, _, err = Parse(nil, strings.NewReader(""))
	expect(t, err, codes.MissingValue)

	_, _, err = Parse([]string{"--url", "://bad", "--path", "src"}, strings.NewReader(""))
	expect(t, err, codes.InvalidURL)

	_, _, err = Parse([]string{"--request", "-", "--url", "https://github.com/org/repo.git"}, strings.NewReader(`{"url":"https://github.com/org/repo.git","path":"src"}`))
	expect(t, err, codes.InvalidInvocation)

	_, _, err = Parse([]string{"--request", filepath.Join(t.TempDir(), "missing.json")}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)

	_, _, err = Parse([]string{"--help"}, nil)
	if !errors.Is(err, ErrHelp) {
		t.Fatal(err)
	}
}

func TestParseJSON(t *testing.T) {
	raw := `{"url":"file:///tmp/git-sync-request-source","path":"dest","unknown":true}`
	_, _, err := Parse([]string{"--request", "-"}, strings.NewReader(raw))
	expect(t, err, codes.MalformedJSON)

	dir := t.TempDir()
	path := filepath.Join(dir, "req.json")
	body := "{\"url\":\"https://github.com/org/repo.git\",\"path\":\"dest\",\"replace\":true}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	opt, _, err := Parse([]string{"--request", path, "--verbose"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if !opt.Replace || opt.Progress == nil {
		t.Fatalf("%+v", opt)
	}
}

func TestParseFormat(t *testing.T) {
	_, format, err := Parse([]string{"--request", "-"}, strings.NewReader(`{"format":"bson"}`))
	expect(t, err, codes.MissingValue)
	if format != "bson" {
		t.Fatalf("format = %s", format)
	}
	_, format, err = Parse([]string{"--format", "yaml"}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)
	if format != "json" {
		t.Fatalf("format = %s", format)
	}
}

func TestParseLimits(t *testing.T) {
	_, _, err := Parse([]string{"--request", "-"}, strings.NewReader(`{"url":"https://example.com/a.git","path":"dest"}{"url":"https://example.com/b.git"}`))
	expect(t, err, codes.MalformedJSON)

	_, _, err = Parse([]string{"--request", "-"}, strings.NewReader(""))
	expect(t, err, codes.MalformedJSON)

	body := `{"url":"https://example.com/a.git","path":"` + strings.Repeat("a", maxRequestBytes) + `"}`
	_, _, err = Parse([]string{"--request", "-"}, strings.NewReader(body))
	expect(t, err, codes.MalformedJSON)

	_, format, err := Parse([]string{"--request", "-", "--format", "bson"}, strings.NewReader(`{"format":"json"}`))
	expect(t, err, codes.InvalidInvocation)
	if format != "bson" {
		t.Fatalf("format = %s", format)
	}

	_, _, err = Parse([]string{"nope"}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)
}

func expect(t *testing.T, err error, code int) {
	t.Helper()
	var ce *codes.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("got %v, want %d", err, code)
	}
}
