package list

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"git-suite/git-list-tags/internal/codes"
)

func TestListTagsByBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "one")
	first := head(t, dir)
	runGit(t, dir, "tag", "base")
	runGit(t, dir, "branch", "develop")
	write(t, filepath.Join(dir, "README"), "two\n")
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "two")
	second := head(t, dir)
	runGit(t, dir, "tag", "-a", "v1.0.0", "-m", "release")

	all, err := List(Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if all.URL != originURL {
		t.Fatalf("url = %s", all.URL)
	}
	if len(all.Branches) != 2 || all.Branches[0].Branch != "develop" || all.Branches[1].Branch != "main" {
		t.Fatalf("branches = %+v", all.Branches)
	}
	if names(all.Branches[0].Tags) != "base" {
		t.Fatalf("develop tags = %+v", all.Branches[0].Tags)
	}
	if names(all.Branches[1].Tags) != "base,v1.0.0" {
		t.Fatalf("main tags = %+v", all.Branches[1].Tags)
	}

	base := all.Branches[0].Tags[0]
	if base.CommitID != first[:8] || base.Annotated || base.Signed || base.TaggerName != "git-list-test" || base.TaggerEmail != "git-list@example.com" || base.When == "" {
		t.Fatalf("lightweight = %+v", base)
	}
	annotated := all.Branches[1].Tags[1]
	if annotated.CommitID != second[:8] || !annotated.Annotated || annotated.Signed || annotated.TaggerName != "git-list-test" || annotated.When == "" {
		t.Fatalf("annotated = %+v", annotated)
	}
	if _, err := time.Parse(time.RFC3339, annotated.When); err != nil {
		t.Fatal(err)
	}

	one, err := List(Options{Path: dir, Branch: "develop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Branches) != 1 || names(one.Branches[0].Tags) != "base" {
		t.Fatalf("one = %+v", one.Branches)
	}
}

func TestListSignedTag(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "one")
	addSignedTag(t, dir, head(t, dir))

	got, err := List(Options{Path: dir, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Branches) != 1 || len(got.Branches[0].Tags) != 1 {
		t.Fatalf("%+v", got.Branches)
	}
	tag := got.Branches[0].Tags[0]
	if tag.Name != "signed" || !tag.Annotated || !tag.Signed || tag.TaggerName != "Ada Lovelace" || tag.TaggerEmail != "ada@example.com" || tag.When != "2020-01-02T03:04:05Z" {
		t.Fatalf("%+v", tag)
	}
}

func TestListRejectsBrokenTags(t *testing.T) {
	dir := committedRepo(t)
	write(t, filepath.Join(dir, ".git", "refs", "tags", "missing"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	_, err := List(Options{Path: dir, Branch: "main"})
	expectCode(t, err, codes.TagUnreadable)

	dir = committedRepo(t)
	write(t, filepath.Join(dir, "blob.txt"), "hello\n")
	blob := strings.TrimSpace(gitOutput(t, dir, "hash-object", "-w", "blob.txt"))
	runGit(t, dir, "update-ref", "refs/tags/on-blob", blob)
	_, err = List(Options{Path: dir})
	expectCode(t, err, codes.TagNotCommit)
}

func TestListRepositoryErrors(t *testing.T) {
	_, err := List(Options{Path: filepath.Join(t.TempDir(), "missing")})
	expectCode(t, err, codes.FolderMissing)

	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "x")
	_, err = List(Options{Path: file})
	expectCode(t, err, codes.NotDirectory)

	_, err = List(Options{Path: t.TempDir()})
	expectCode(t, err, codes.NotRepository)

	bare := t.TempDir()
	runGit(t, bare, "init", "-b", "main")
	write(t, filepath.Join(bare, "README"), "one\n")
	runGit(t, bare, "add", "README")
	runGit(t, bare, "commit", "-m", "one")
	_, err = List(Options{Path: bare, Branch: "main"})
	expectCode(t, err, codes.NoRemoteURL)

	dir := committedRepo(t)
	_, err = List(Options{Path: dir, Branch: "no-such"})
	expectCode(t, err, codes.BranchNotFound)
	_, err = List(Options{Path: dir, Branch: "a..b"})
	expectCode(t, err, codes.InvalidBranch)
}

func addOrigin(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "remote", "add", "origin", originURL)
}

const originURL = "https://example.com/repo.git"

func addSignedTag(t *testing.T, dir, commit string) {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	tag := &object.Tag{
		Name: "signed",
		Tagger: object.Signature{
			Name:  "Ada Lovelace",
			Email: "ada@example.com",
			When:  time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		Message:      "signed release\n",
		TargetType:   plumbing.CommitObject,
		Target:       plumbing.NewHash(commit),
		PGPSignature: "-----BEGIN PGP SIGNATURE-----\n\n-----END PGP SIGNATURE-----\n",
	}
	obj := repo.Storer.NewEncodedObject()
	if err := tag.Encode(obj); err != nil {
		t.Fatal(err)
	}
	hash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	ref := plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/signed"), hash)
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}
}

func committedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	write(t, filepath.Join(dir, "README"), "one\n")
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "-m", "one")
	return dir
}

func names(tags []Tag) string {
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = tag.Name
	}
	return strings.Join(parts, ",")
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
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=git-list-test",
		"GIT_AUTHOR_EMAIL=git-list@example.com",
		"GIT_COMMITTER_NAME=git-list-test",
		"GIT_COMMITTER_EMAIL=git-list@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

func head(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
