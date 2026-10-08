package info

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-git/go-git/v5"

	"git-suite/git-get-branch-info/internal/codes"
)

func TestCollectDetachedHeadAndSharedTip(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	runGit(t, dir, "branch", "develop")
	runGit(t, dir, "checkout", "--detach")

	got, err := Collect(Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Branches) != 2 {
		t.Fatalf("%+v", got.Branches)
	}
	for _, branch := range got.Branches {
		if branch.CheckedOut || branch.CommitCount != 1 || branch.LastCommitID != branch.FirstCommitID {
			t.Fatalf("branch = %+v", branch)
		}
	}
}

func TestCollectSubjectAndHistory(t *testing.T) {
	long := strings.Repeat("é", 800)
	if got := clipText(long); !utf8.ValidString(got) || len(got) > 1024 || !strings.HasSuffix(got, "...") {
		t.Fatalf("clip = %q (%d)", got, len(got))
	}
	if got := subject("  two\r\nbody\n"); got != "two" {
		t.Fatalf("subject = %q", got)
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	message := filepath.Join(dir, "message.txt")
	write(t, message, "two\r\nrest of the message\n")
	runGitAt(t, dir, "2020-03-01T00:00:00Z", "commit", "-F", message)
	got, err := Collect(Options{Path: dir, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Branches[0].LastCommitSubject != "two" || got.Branches[0].CommitCount != 1 {
		t.Fatalf("%+v", got.Branches[0])
	}

	for i := 0; i < 24; i++ {
		write(t, filepath.Join(dir, "README"), strings.Repeat("x", i+1))
		runGit(t, dir, "add", "README")
		runGitAt(t, dir, "2021-01-01T00:00:00Z", "commit", "-m", "step")
	}
	got, err = Collect(Options{Path: dir, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Branches[0].CommitCount != 25 || got.Branches[0].RootCount != 1 {
		t.Fatalf("count = %+v", got.Branches[0])
	}

	huge := filepath.Join(dir, "huge.txt")
	write(t, huge, strings.Repeat("é", 800)+"\nbody\n")
	runGit(t, dir, "commit", "-F", huge, "--allow-empty")
	got, err = Collect(Options{Path: dir, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	subject := got.Branches[0].LastCommitSubject
	if !utf8.ValidString(subject) || len(subject) > 1024 || !strings.HasSuffix(subject, "...") {
		t.Fatalf("subject length %d %q", len(subject), subject[:40])
	}
}

func TestCollectRepositoryEdges(t *testing.T) {
	_, err := Collect(Options{Path: "a\x00b"})
	expectCode(t, err, codes.InvalidPath)
	_, err = Collect(Options{Path: "/"})
	expectCode(t, err, codes.InvalidPath)

	unborn := t.TempDir()
	runGit(t, unborn, "init", "-b", "main")
	addOrigin(t, unborn)
	_, err = Collect(Options{Path: unborn})
	expectCode(t, err, codes.NoBranches)

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGitAt(t, dir, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Storer.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remotes["origin"].URLs = []string{"   "}
	if err := repo.Storer.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	_, err = Collect(Options{Path: dir})
	expectCode(t, err, codes.NoRemoteURL)

	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init", "-b", "main")
	addOrigin(t, src)
	write(t, filepath.Join(src, "README"), "one\n")
	runGit(t, src, "add", "README")
	runGitAt(t, src, "2020-01-01T00:00:00Z", "commit", "-m", "one")
	var stable Result
	for i := 0; i < 8; i++ {
		got, err := Collect(Options{Path: src})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			stable = got
			continue
		}
		if got.URL != stable.URL || got.Branches[0].CommitCount != stable.Branches[0].CommitCount {
			t.Fatalf("repeat = %+v", got)
		}
	}
}
