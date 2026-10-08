package info

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-get-branch-info/internal/codes"
)

func TestCollectBranches(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	first := head(t, dir)
	runGit(t, dir, "branch", "develop")
	write(t, filepath.Join(dir, "README"), "two\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-02-01T00:00:00Z", "commit", "-m", "two\n\nbody")
	second := head(t, dir)

	all, err := Collect(Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if all.URL != originURL {
		t.Fatalf("url = %s", all.URL)
	}
	if len(all.Branches) != 2 || all.Branches[0].Branch != "develop" || all.Branches[1].Branch != "main" {
		t.Fatalf("%+v", all.Branches)
	}
	develop := all.Branches[0]
	if develop.CheckedOut || develop.CommitCount != 1 || develop.RootCount != 1 ||
		develop.FirstCommitID != first[:8] || develop.LastCommitID != first[:8] ||
		develop.FirstCommitAt != "2020-01-01T00:00:00Z" || develop.LastCommitSubject != "one" ||
		develop.FirstCommitterName != "git-info-test" {
		t.Fatalf("develop = %+v", develop)
	}
	main := all.Branches[1]
	if !main.CheckedOut || main.CommitCount != 2 || main.RootCount != 1 ||
		main.FirstCommitID != first[:8] || main.LastCommitID != second[:8] ||
		main.FirstCommitAt != "2020-01-01T00:00:00Z" || main.LastCommitAt != "2020-02-01T00:00:00Z" ||
		main.LastCommitSubject != "two" || main.LastCommitterEmail != "git-info@example.com" {
		t.Fatalf("main = %+v", main)
	}

	one, err := Collect(Options{Path: dir, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Branches) != 1 || one.Branches[0].Branch != "develop" || one.Branches[0].CommitCount != 1 {
		t.Fatalf("%+v", one.Branches)
	}
}

func TestCollectMultipleRoots(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	first := head(t, dir)
	runGit(t, dir, "checkout", "--orphan", "side")
	runGitAt(t, dir, "2020-06-01T00:00:00Z", "commit", "--allow-empty", "-m", "side")
	runGit(t, dir, "checkout", "main")
	runGitAt(t, dir, "2020-12-01T00:00:00Z", "merge", "--allow-unrelated-histories", "-m", "merge", "side")

	got, err := Collect(Options{Path: dir, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	branch := got.Branches[0]
	if branch.CommitCount != 3 || branch.RootCount != 2 || branch.FirstCommitID != first[:8] ||
		branch.FirstCommitAt != "2020-01-01T00:00:00Z" || branch.LastCommitAt != "2020-12-01T00:00:00Z" ||
		branch.LastCommitSubject != "merge" || !branch.CheckedOut {
		t.Fatalf("%+v", branch)
	}
}

func TestCollectRepositoryErrors(t *testing.T) {
	_, err := Collect(Options{Path: filepath.Join(t.TempDir(), "missing")})
	expectCode(t, err, codes.FolderMissing)

	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "x")
	_, err = Collect(Options{Path: file})
	expectCode(t, err, codes.NotDirectory)

	_, err = Collect(Options{Path: t.TempDir()})
	expectCode(t, err, codes.NotRepository)

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	_, err = Collect(Options{Path: dir, Branch: "main"})
	expectCode(t, err, codes.NoRemoteURL)
	addOrigin(t, dir)
	_, err = Collect(Options{Path: dir, Branch: "no-such"})
	expectCode(t, err, codes.BranchNotFound)
	_, err = Collect(Options{Path: dir, Branch: "a..b"})
	expectCode(t, err, codes.InvalidBranch)
}

const originURL = "https://example.com/repo.git"

func addOrigin(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "remote", "add", "origin", originURL)
}

func expectCode(t *testing.T, err error, code int) {
	t.Helper()
	var ce *codes.Error
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want code %d", err, code)
	}
	if ce.Code != code {
		t.Fatalf("code = %d (%s), want %d", ce.Code, ce.Message, code)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runGitAt(t, dir, "", args...)
}

func runGitAt(t *testing.T, dir, when string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to build test repositories")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if when != "" {
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=git-info-test",
		"GIT_AUTHOR_EMAIL=git-info@example.com",
		"GIT_COMMITTER_NAME=git-info-test",
		"GIT_COMMITTER_EMAIL=git-info@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

func head(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = gitEnv()
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
