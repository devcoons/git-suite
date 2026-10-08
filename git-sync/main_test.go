package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRunContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(""), &stdout, &stderr); code != -1 {
		t.Fatalf("code = %d", code)
	}
	var failed struct {
		Status  string `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.Code != 102 || failed.Message == "" {
		t.Fatalf("%+v", failed)
	}
	if strings.Count(strings.TrimSuffix(stdout.String(), "\n"), "\n") != 0 {
		t.Fatalf("stdout has more than one line: %s", stdout.String())
	}

	stdout.Reset()
	if code := run([]string{"--help"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("help code = %d", code)
	}
	if !strings.Contains(stdout.String(), "git-sync") {
		t.Fatalf("help = %s", stdout.String())
	}

	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, src, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(src, "README"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, src, "add", "README")
	gitCmd(t, src, "commit", "-m", "init")
	abs, err := filepath.Abs(src)
	if err != nil {
		t.Fatal(err)
	}
	raw := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	dest := filepath.Join(t.TempDir(), "out")
	body := `{"url":"` + raw + `","path":"` + dest + `"}`
	stdout.Reset()
	if code := run([]string{"--request", "-"}, strings.NewReader(body), &stdout, &stderr); code != 0 {
		t.Fatalf("sync code = %d stdout=%s", code, stdout.String())
	}
	var okDoc struct {
		Status string `json:"status"`
		Action string `json:"action"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &okDoc); err != nil {
		t.Fatal(err)
	}
	if okDoc.Status != "ok" || okDoc.Action != "cloned" || len(okDoc.Commit) != 8 {
		t.Fatalf("%+v", okDoc)
	}
	if bytes.Contains(stdout.Bytes(), []byte(": ")) || bytes.Count(stdout.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("json is not packed: %s", stdout.Bytes())
	}

	dest2 := filepath.Join(t.TempDir(), "out-bson")
	body = `{"url":"` + raw + `","path":"` + dest2 + `","format":"bson"}`
	stdout.Reset()
	if code := run([]string{"--request", "-"}, strings.NewReader(body), &stdout, &stderr); code != 0 {
		t.Fatalf("bson sync code = %d", code)
	}
	var bsonDoc struct {
		Status string `json:"status"`
		Path   string `json:"path"`
		URL    string `json:"url"`
		Action string `json:"action"`
	}
	decodeBSON(t, stdout.Bytes(), &bsonDoc)
	if bsonDoc.Status != "ok" || bsonDoc.Path != dest2 || bsonDoc.URL != raw || bsonDoc.Action != "cloned" {
		t.Fatalf("%+v", bsonDoc)
	}
}

func TestFailureUsesRequestedFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--format", "bson"}, strings.NewReader(""), &stdout, &stderr); code != -1 {
		t.Fatalf("code = %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
	var doc struct {
		Status  string `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	decodeBSON(t, stdout.Bytes(), &doc)
	if doc.Status != "failed" || doc.Code != 102 || doc.Message == "" {
		t.Fatalf("%+v", doc)
	}

	stdout.Reset()
	if code := run([]string{"--request", "-", "--format", "bson"}, strings.NewReader(`{"format":"json"}`), &stdout, &stderr); code != -1 {
		t.Fatalf("mismatch code = %d stdout=%s", code, stdout.String())
	}
	doc = struct {
		Status  string `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{}
	decodeBSON(t, stdout.Bytes(), &doc)
	if doc.Status != "failed" || doc.Code != 100 {
		t.Fatalf("%+v", doc)
	}
}

func decodeBSON(t *testing.T, raw []byte, dest any) {
	t.Helper()
	if bytes.Contains(raw, []byte("\n")) {
		t.Fatal("bson document contains a newline")
	}
	dec := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(raw)))
	dec.UseJSONStructTags()
	if err := dec.Decode(dest); err != nil {
		t.Fatal(err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=git-sync-test",
		"GIT_AUTHOR_EMAIL=git-sync@example.com",
		"GIT_COMMITTER_NAME=git-sync-test",
		"GIT_COMMITTER_EMAIL=git-sync@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
