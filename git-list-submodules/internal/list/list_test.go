package list

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-suite/git-list-submodules/internal/codes"
)

func TestListReadsBranchCommit(t *testing.T) {
	parent := t.TempDir()
	runGit(t, parent, "init", "-b", "main")
	addOrigin(t, parent)
	write(t, filepath.Join(parent, "README"), "root\n")
	runGit(t, parent, "add", "README")
	runGit(t, parent, "commit", "-m", "init")
	runGit(t, parent, "branch", "develop")

	sha := sideCommit(t)
	write(t, filepath.Join(parent, ".gitmodules"), ""+
		"[submodule \"libs/sub\"]\n"+
		"\tpath = libs/sub\n"+
		"\turl = https://example.com/sub.git\n")
	runGit(t, parent, "add", ".gitmodules")
	runGit(t, parent, "update-index", "--add", "--cacheinfo", "160000,"+sha+",libs/sub")
	runGit(t, parent, "commit", "-m", "add submodule")

	if _, err := os.Stat(filepath.Join(parent, "libs", "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture checked out the submodule")
	}

	all, err := List(Options{Path: parent})
	if err != nil {
		t.Fatal(err)
	}
	if all.URL != originURL {
		t.Fatalf("url = %s", all.URL)
	}
	if len(all.Branches) != 2 {
		t.Fatalf("branches = %+v", all.Branches)
	}
	if all.Branches[0].Branch != "develop" || len(all.Branches[0].Submodules) != 0 {
		t.Fatalf("develop = %+v", all.Branches[0])
	}
	got := all.Branches[1]
	if got.Branch != "main" || len(got.Submodules) != 1 {
		t.Fatalf("main = %+v", got)
	}
	sub := got.Submodules[0]
	if sub.URL != "https://example.com/sub.git" || sub.Path != "libs/sub" || sub.CommitID != sha[:8] {
		t.Fatalf("submodule = %+v, sha %s", sub, sha)
	}

	one, err := List(Options{Path: parent, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Branches) != 1 || one.Branches[0].Branch != "develop" || len(one.Branches[0].Submodules) != 0 {
		t.Fatalf("one = %+v", one.Branches)
	}
}

func TestListRejectsIncompleteRecords(t *testing.T) {
	missing := repoWith(t, ""+
		"[submodule \"libs/gone\"]\n"+
		"\tpath = libs/gone\n"+
		"\turl = https://example.com/gone.git\n", "")
	_, err := List(Options{Path: missing})
	expectCode(t, err, codes.SubmoduleMissing)

	sha := sideCommit(t)
	orphan := repoWith(t, "", sha)
	_, err = List(Options{Path: orphan, Branch: "main"})
	expectCode(t, err, codes.SubmoduleURL)
}

func TestListRepositoryErrors(t *testing.T) {
	_, err := List(Options{Path: filepath.Join(t.TempDir(), "missing")})
	expectCode(t, err, codes.FolderMissing)

	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "x")
	_, err = List(Options{Path: file})
	expectCode(t, err, codes.NotDirectory)

	empty := t.TempDir()
	_, err = List(Options{Path: empty})
	expectCode(t, err, codes.NotRepository)

	parent := t.TempDir()
	runGit(t, parent, "init", "-b", "main")
	write(t, filepath.Join(parent, "README"), "root\n")
	runGit(t, parent, "add", "README")
	runGit(t, parent, "commit", "-m", "init")
	_, err = List(Options{Path: parent, Branch: "main"})
	expectCode(t, err, codes.NoRemoteURL)
	addOrigin(t, parent)
	_, err = List(Options{Path: parent, Branch: "no-such"})
	expectCode(t, err, codes.BranchNotFound)

	_, err = List(Options{Path: parent, Branch: "a..b"})
	expectCode(t, err, codes.InvalidBranch)
}

func repoWith(t *testing.T, modules, gitlink string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "root\n")
	runGit(t, dir, "add", "README")
	if modules != "" {
		write(t, filepath.Join(dir, ".gitmodules"), modules)
		runGit(t, dir, "add", ".gitmodules")
	}
	if gitlink != "" {
		runGit(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+gitlink+",libs/sub")
	}
	runGit(t, dir, "commit", "-m", "record")
	return dir
}

const originURL = "https://example.com/repo.git"

func addOrigin(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "remote", "add", "origin", originURL)
}

func sideCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	write(t, filepath.Join(dir, "README"), "sub\n")
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "sub")
	return head(t, dir)
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
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required to build test repositories")
	}
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
