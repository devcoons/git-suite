package syncer

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-sync/internal/codes"
)

func TestSyncCloneUpdateAndReplace(t *testing.T) {
	src, srcURL := makeRepo(t, "v1\n")
	dest := filepath.Join(t.TempDir(), "checkout")

	first, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != ActionCloned || first.Branch != "main" || first.Commit != head(t, src)[:8] {
		t.Fatalf("first = %+v", first)
	}
	if got := readFile(t, filepath.Join(dest, "README")); got != "v1\n" {
		t.Fatalf("README = %q", got)
	}

	second, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != ActionUnchanged || second.Commit != first.Commit {
		t.Fatalf("second = %+v", second)
	}

	write(t, filepath.Join(src, "README"), "v2\n")
	runGit(t, src, "add", "README")
	runGit(t, src, "commit", "-m", "v2")
	write(t, filepath.Join(dest, "README"), "dirty\n")
	write(t, filepath.Join(dest, "extra.txt"), "drop me\n")
	if err := os.MkdirAll(filepath.Join(dest, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dest, "scratch", "note"), "x\n")

	third, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if third.Action != ActionUpdated || third.Commit != head(t, src)[:8] {
		t.Fatalf("third = %+v, source %s", third, head(t, src))
	}
	if got := readFile(t, filepath.Join(dest, "README")); got != "v2\n" {
		t.Fatalf("README = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "extra.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked file remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "scratch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked directory remains: %v", err)
	}

	other, otherURL := makeRepo(t, "other\n")
	swapped, err := Sync(context.Background(), Options{URL: otherURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if swapped.Action != ActionCloned || swapped.Commit != head(t, other)[:8] {
		t.Fatalf("swap = %+v", swapped)
	}
	if got := readFile(t, filepath.Join(dest, "README")); got != "other\n" {
		t.Fatalf("README = %q", got)
	}
	kept := readFile(t, filepath.Join(dest, "README"))
	missing := filepath.Join(t.TempDir(), "missing-source")
	missingURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(missing)}).String()
	_, err = Sync(context.Background(), Options{URL: missingURL, Path: dest})
	if err == nil {
		t.Fatal("expected a failed clone to return an error")
	}
	if got := readFile(t, filepath.Join(dest, "README")); got != kept {
		t.Fatalf("failed clone changed README to %q", got)
	}
}

func TestSyncRefusesForeignDirectory(t *testing.T) {
	_, srcURL := makeRepo(t, "v1\n")
	dest := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dest, "keep.txt"), "keep\n")

	_, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	expectCode(t, err, codes.RefusingReplace)
	if got := readFile(t, filepath.Join(dest, "keep.txt")); got != "keep\n" {
		t.Fatalf("keep.txt = %q", got)
	}

	res, err := Sync(context.Background(), Options{URL: srcURL, Path: dest, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionCloned {
		t.Fatalf("action = %s", res.Action)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replace left the previous file in place")
	}
}

func TestSyncBranch(t *testing.T) {
	src, srcURL := makeRepo(t, "main\n")
	runGit(t, src, "checkout", "-b", "develop")
	write(t, filepath.Join(src, "README"), "develop\n")
	runGit(t, src, "add", "README")
	runGit(t, src, "commit", "-m", "develop")
	runGit(t, src, "checkout", "main")

	dest := filepath.Join(t.TempDir(), "checkout")
	res, err := Sync(context.Background(), Options{URL: srcURL, Path: dest, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "develop" || readFile(t, filepath.Join(dest, "README")) != "develop\n" {
		t.Fatalf("result = %+v", res)
	}

	_, err = Sync(context.Background(), Options{URL: srcURL, Path: dest, Branch: "missing"})
	expectCode(t, err, codes.BranchNotFound)
	if readFile(t, filepath.Join(dest, "README")) != "develop\n" {
		t.Fatal("unknown branch replaced the checkout")
	}
}

func TestSyncEmptyDirectoryAndFilePath(t *testing.T) {
	_, srcURL := makeRepo(t, "v1\n")
	parent := t.TempDir()
	dest := filepath.Join(parent, "empty")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionCloned {
		t.Fatalf("action = %s", res.Action)
	}

	filePath := filepath.Join(parent, "file")
	write(t, filePath, "x")
	_, err = Sync(context.Background(), Options{URL: srcURL, Path: filePath})
	expectCode(t, err, codes.NotDirectory)
}

func makeRepo(t *testing.T, readme string) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	write(t, filepath.Join(dir, "README"), readme)
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "init")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	return dir, raw
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to build test repositories")
	}
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

func head(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
