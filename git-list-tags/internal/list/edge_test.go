package list

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"git-suite/git-list-tags/internal/codes"
)

func TestListTagDepthAndEmptyTags(t *testing.T) {
	dir := committedRepo(t)
	got, err := List(Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Branches) != 1 || len(got.Branches[0].Tags) != 0 {
		t.Fatalf("%+v", got.Branches)
	}

	shallow := committedRepo(t)
	nestTags(t, shallow, head(t, shallow), 9)
	got, err = List(Options{Path: shallow, Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Branches[0].Tags) != 1 || got.Branches[0].Tags[0].Name != "deep" || !got.Branches[0].Tags[0].Annotated {
		t.Fatalf("%+v", got.Branches[0].Tags)
	}

	deep := committedRepo(t)
	nestTags(t, deep, head(t, deep), 10)
	_, err = List(Options{Path: deep})
	expectCode(t, err, codes.TagUnreadable)
}

func TestListTextLimitsAndRepeatedCalls(t *testing.T) {
	long := strings.Repeat("é", 800)
	if got := clipText(long); !utf8.ValidString(got) || len(got) > 1024 || !strings.HasSuffix(got, "...") {
		t.Fatalf("clip = %q (%d)", got, len(got))
	}

	dir := committedRepo(t)
	var first Result
	for i := 0; i < 8; i++ {
		got, err := List(Options{Path: dir})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
		}
		if got.URL != first.URL || got.Path != first.Path || len(got.Branches) != 1 {
			t.Fatalf("repeat = %+v", got)
		}
	}

	_, err := List(Options{Path: "a\x00b"})
	expectCode(t, err, codes.InvalidPath)
	_, err = List(Options{Path: "/"})
	expectCode(t, err, codes.InvalidPath)

	unborn := t.TempDir()
	runGit(t, unborn, "init", "-b", "main")
	addOrigin(t, unborn)
	_, err = List(Options{Path: unborn})
	expectCode(t, err, codes.NoBranches)
}

func nestTags(t *testing.T, dir, commit string, depth int) {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := plumbing.NewHash(commit)
	targetType := plumbing.CommitObject
	var hash plumbing.Hash
	for i := 0; i < depth; i++ {
		tag := &object.Tag{
			Name:       "deep",
			Tagger:     object.Signature{Name: "Ada", Email: "ada@example.com", When: time.Unix(1_700_000_000, 0).UTC()},
			Message:    "deep\n",
			TargetType: targetType,
			Target:     target,
		}
		obj := repo.Storer.NewEncodedObject()
		if err := tag.Encode(obj); err != nil {
			t.Fatal(err)
		}
		hash, err = repo.Storer.SetEncodedObject(obj)
		if err != nil {
			t.Fatal(err)
		}
		target = hash
		targetType = plumbing.TagObject
	}
	ref := plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/deep"), hash)
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}
}
