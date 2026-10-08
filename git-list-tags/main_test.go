package main

import (
	"bytes"
	"encoding/json"
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

	stdout.Reset()
	if code := run([]string{"--help"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("help code = %d", code)
	}
	if !strings.Contains(stdout.String(), "git-list-tags") {
		t.Fatalf("help = %s", stdout.String())
	}

	dir := t.TempDir()
	gitCmd(t, dir, "init", "-b", "main")
	gitCmd(t, dir, "remote", "add", "origin", "https://example.com/repo.git")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "README")
	gitCmd(t, dir, "commit", "-m", "init")

	stdout.Reset()
	body := `{"path":"` + dir + `"}`
	if code := run([]string{"--request", "-"}, strings.NewReader(body), &stdout, &stderr); code != 0 {
		t.Fatalf("list code = %d stdout=%s", code, stdout.String())
	}
	var okDoc struct {
		Status   string `json:"status"`
		Path     string `json:"path"`
		URL      string `json:"url"`
		Branches []struct {
			Branch string `json:"branch"`
			Tags   []struct {
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"branches"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &okDoc); err != nil {
		t.Fatal(err)
	}
	if okDoc.Status != "ok" || okDoc.Path != dir || okDoc.URL != "https://example.com/repo.git" ||
		len(okDoc.Branches) != 1 || okDoc.Branches[0].Branch != "main" {
		t.Fatalf("%+v", okDoc)
	}
	if okDoc.Branches[0].Tags == nil || len(okDoc.Branches[0].Tags) != 0 {
		t.Fatalf("tags = %+v", okDoc.Branches[0].Tags)
	}
	if bytes.Contains(stdout.Bytes(), []byte(": ")) || bytes.Count(stdout.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("json is not packed: %s", stdout.Bytes())
	}

	stdout.Reset()
	if code := run([]string{"--path", dir, "--format", "bson"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("bson list code = %d", code)
	}
	var bsonDoc struct {
		Status string `json:"status"`
		Path   string `json:"path"`
		URL    string `json:"url"`
	}
	decodeBSON(t, stdout.Bytes(), &bsonDoc)
	if bsonDoc.Status != "ok" || bsonDoc.Path != dir || bsonDoc.URL != "https://example.com/repo.git" {
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
		"GIT_AUTHOR_NAME=git-list-test",
		"GIT_AUTHOR_EMAIL=git-list@example.com",
		"GIT_COMMITTER_NAME=git-list-test",
		"GIT_COMMITTER_EMAIL=git-list@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
