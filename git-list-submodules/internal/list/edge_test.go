package list

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"git-suite/git-list-submodules/internal/codes"
)

func TestListEdgeCases(t *testing.T) {
	_, err := List(Options{Path: "a\x00b"})
	expectCode(t, err, codes.InvalidPath)
	_, err = List(Options{Path: "/"})
	expectCode(t, err, codes.InvalidPath)

	dangling := filepath.Join(t.TempDir(), "missing-link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), dangling); err != nil {
		t.Fatal(err)
	}
	_, err = List(Options{Path: dangling})
	expectCode(t, err, codes.FolderMissing)

	unborn := t.TempDir()
	runGit(t, unborn, "init", "-b", "main")
	addOrigin(t, unborn)
	_, err = List(Options{Path: unborn})
	expectCode(t, err, codes.NoBranches)

	dir := repoWith(t, ""+
		"[submodule \"a\"]\n"+
		"\tpath = libs/sub\n"+
		"\turl = ../sub.git\n"+
		"[submodule \"b\"]\n"+
		"\tpath = libs/sub\n"+
		"\turl = https://example.com/b.git\n", sideCommit(t))
	_, err = List(Options{Path: dir})
	expectCode(t, err, codes.ModulesInvalid)

	escaped := repoWith(t, ""+
		"[submodule \"out\"]\n"+
		"\tpath = ../outside\n"+
		"\turl = https://example.com/out.git\n", "")
	_, err = List(Options{Path: escaped})
	expectCode(t, err, codes.ModulesInvalid)

	absolute := repoWith(t, ""+
		"[submodule \"abs\"]\n"+
		"\tpath = /tmp/outside\n"+
		"\turl = https://example.com/out.git\n", "")
	_, err = List(Options{Path: absolute})
	expectCode(t, err, codes.SubmodulePath)

	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init", "-b", "main")
	addOrigin(t, src)
	write(t, filepath.Join(src, "README"), "root\n")
	runGit(t, src, "add", "README")
	runGit(t, src, "commit", "-m", "init")
	link := filepath.Join(parent, "link")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	viaLink, err := List(Options{Path: link})
	if err != nil {
		t.Fatal(err)
	}
	if viaLink.Status != "ok" || len(viaLink.Branches) != 1 || viaLink.URL != originURL {
		t.Fatalf("%+v", viaLink)
	}

	for i := 0; i < 8; i++ {
		again, err := List(Options{Path: src})
		if err != nil {
			t.Fatal(err)
		}
		if again.URL != originURL || len(again.Branches) != 1 || again.Branches[0].Branch != "main" {
			t.Fatalf("repeat = %+v", again)
		}
	}
}

func TestListRejectsOversizedModules(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	addOrigin(t, dir)
	body := "[submodule \"big\"]\n\tpath = libs/sub\n\turl = https://example.com/" + strings.Repeat("a", maxModulesBytes) + ".git\n"
	write(t, filepath.Join(dir, ".gitmodules"), body)
	runGit(t, dir, "add", ".gitmodules")
	runGit(t, dir, "commit", "-m", "big")
	_, err := List(Options{Path: dir})
	expectCode(t, err, codes.ModulesInvalid)
}

func TestListUsesFirstOriginURL(t *testing.T) {
	dir := repoWith(t, "", "")
	runGit(t, dir, "remote", "set-url", "--add", "origin", "https://example.com/second.git")
	got, err := List(Options{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != originURL {
		t.Fatalf("url = %s", got.URL)
	}
}

func TestListRejectsBlankOriginURL(t *testing.T) {
	dir := repoWith(t, "", "")
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
	_, err = List(Options{Path: dir})
	expectCode(t, err, codes.NoRemoteURL)
}
