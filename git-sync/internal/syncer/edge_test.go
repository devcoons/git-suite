package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-sync/internal/codes"
)

func TestSyncDiscardsLocalCommitsAndSwitchesBranch(t *testing.T) {
	src, srcURL := makeRepo(t, "main\n")
	runGit(t, src, "checkout", "-b", "develop")
	write(t, filepath.Join(src, "README"), "develop\n")
	runGit(t, src, "add", "README")
	runGit(t, src, "commit", "-m", "develop")
	runGit(t, src, "checkout", "main")

	parent := t.TempDir()
	dest := filepath.Join(parent, "checkout")
	if _, err := Sync(context.Background(), Options{URL: srcURL, Path: dest}); err != nil {
		t.Fatal(err)
	}
	runGit(t, dest, "commit", "--allow-empty", "-m", "local only")

	refreshed, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Action != ActionUpdated || refreshed.Commit != head(t, src)[:8] || refreshed.Branch != "main" {
		t.Fatalf("refresh = %+v, source %s", refreshed, head(t, src))
	}
	if subject := gitOutput(t, dest, "log", "-1", "--format=%s"); subject != "init" {
		t.Fatalf("HEAD subject = %q", subject)
	}

	switched, err := Sync(context.Background(), Options{URL: srcURL, Path: dest, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if switched.Action != ActionUpdated || switched.Branch != "develop" || readFile(t, filepath.Join(dest, "README")) != "develop\n" {
		t.Fatalf("switch = %+v", switched)
	}
	again, err := Sync(context.Background(), Options{URL: srcURL, Path: dest, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Action != ActionUnchanged || again.Commit != switched.Commit {
		t.Fatalf("second switch = %+v", again)
	}
	assertNoSyncTemps(t, parent)
}

func TestSyncFailedCloneLeavesNoTemporaryDirectory(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "checkout")
	missing := filepath.Join(t.TempDir(), "missing")
	missingURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(missing)}).String()
	if _, err := Sync(context.Background(), Options{URL: missingURL, Path: dest}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination created despite failure: %v", err)
	}
	assertNoSyncTemps(t, parent)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, srcURL := makeRepo(t, "v1\n")
	if _, err := Sync(ctx, Options{URL: srcURL, Path: filepath.Join(parent, "late")}); err == nil {
		t.Fatal("expected a cancelled sync to fail")
	}
	assertNoSyncTemps(t, parent)
}

func TestSyncRejectsUnsafePaths(t *testing.T) {
	src, srcURL := makeRepo(t, "v1\n")
	_, err := Sync(context.Background(), Options{URL: srcURL, Path: src})
	expectCode(t, err, codes.InvalidPath)
	_, err = Sync(context.Background(), Options{URL: srcURL, Path: filepath.Join(src, "nested")})
	expectCode(t, err, codes.InvalidPath)
	_, err = Sync(context.Background(), Options{URL: srcURL, Path: filepath.Dir(src)})
	expectCode(t, err, codes.InvalidPath)
	if readFile(t, filepath.Join(src, "README")) != "v1\n" {
		t.Fatal("rejected sync changed the source")
	}

	parent := t.TempDir()
	blocker := filepath.Join(parent, "not-a-directory")
	write(t, blocker, "x")
	_, err = Sync(context.Background(), Options{URL: srcURL, Path: filepath.Join(blocker, "repo")})
	expectCode(t, err, codes.Destination)

	expectCode(t, Check(Options{URL: srcURL, Path: "a\x00b"}), codes.InvalidPath)
	expectCode(t, Check(Options{URL: srcURL, Path: "/"}), codes.InvalidPath)

	link := filepath.Join(parent, "link")
	if err := os.Symlink(blocker, link); err != nil {
		t.Fatal(err)
	}
	_, err = Sync(context.Background(), Options{URL: srcURL, Path: link})
	expectCode(t, err, codes.NotDirectory)
}

func TestSyncRefusesPopulatedDirectoryWithoutReadingEveryEntry(t *testing.T) {
	_, srcURL := makeRepo(t, "v1\n")
	dest := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		write(t, filepath.Join(dest, fmt.Sprintf("file-%02d", i)), "x")
	}
	write(t, filepath.Join(dest, "keep.txt"), "keep\n")
	_, err := Sync(context.Background(), Options{URL: srcURL, Path: dest})
	expectCode(t, err, codes.RefusingReplace)
	if readFile(t, filepath.Join(dest, "keep.txt")) != "keep\n" {
		t.Fatal("populated directory was replaced")
	}
}

func assertNoSyncTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".git-sync") {
			t.Fatalf("temporary directory remains: %s", entry.Name())
		}
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
