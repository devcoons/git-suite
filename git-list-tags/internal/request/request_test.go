package request

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-list-tags/internal/codes"
)

func TestParse(t *testing.T) {
	opt, _, err := Parse([]string{"--path", "src/repo", "--branch", "main"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if opt.Branch != "main" || opt.Path == "" {
		t.Fatalf("%+v", opt)
	}

	_, _, err = Parse(nil, strings.NewReader(""))
	expect(t, err, codes.MissingValue)

	_, _, err = Parse([]string{"--path", "src", "--branch", "a..b"}, strings.NewReader(""))
	expect(t, err, codes.InvalidBranch)

	_, _, err = Parse([]string{"--request", "-", "--path", "src"}, strings.NewReader(`{"path":"src"}`))
	expect(t, err, codes.InvalidInvocation)

	_, _, err = Parse([]string{"--request", filepath.Join(t.TempDir(), "missing.json")}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)

	_, _, err = Parse([]string{"--help"}, nil)
	if !errors.Is(err, ErrHelp) {
		t.Fatal(err)
	}
}

func TestParseJSON(t *testing.T) {
	_, _, err := Parse([]string{"--request", "-"}, strings.NewReader(`{"path":"src","unknown":true}`))
	expect(t, err, codes.MalformedJSON)

	dir := t.TempDir()
	path := filepath.Join(dir, "req.json")
	if err := os.WriteFile(path, []byte("{\"path\":\"dest\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt, _, err := Parse([]string{"--request", path, "--verbose"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if opt.Branch != "" {
		t.Fatalf("%+v", opt)
	}
}

func TestParseFormat(t *testing.T) {
	_, format, err := Parse([]string{"--request", "-", "--format", "bson"}, strings.NewReader(`{"path":"src"}`))
	if err != nil || format != "bson" {
		t.Fatalf("format %s err %v", format, err)
	}
	_, format, err = Parse([]string{"--format", "yaml"}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)
	if format != "json" {
		t.Fatalf("format = %s", format)
	}
}

func TestParseLimits(t *testing.T) {
	_, _, err := Parse([]string{"--request", "-"}, strings.NewReader(`{"path":"src"}{"path":"other"}`))
	expect(t, err, codes.MalformedJSON)
	_, _, err = Parse([]string{"--request", "-"}, strings.NewReader(""))
	expect(t, err, codes.MalformedJSON)
	body := `{"path":"` + strings.Repeat("a", maxRequestBytes) + `"}`
	_, _, err = Parse([]string{"--request", "-"}, strings.NewReader(body))
	expect(t, err, codes.MalformedJSON)
	_, format, err := Parse([]string{"--request", "-", "--format", "bson"}, strings.NewReader(`{"path":"src","format":"json"}`))
	expect(t, err, codes.InvalidInvocation)
	if format != "bson" {
		t.Fatalf("format = %s", format)
	}
	_, _, err = Parse([]string{"--path", "src", "extra"}, strings.NewReader(""))
	expect(t, err, codes.InvalidInvocation)
}

func expect(t *testing.T, err error, code int) {
	t.Helper()
	var ce *codes.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("got %v, want %d", err, code)
	}
}
