// Package list reads the tags contained in one local branch, or in every
// local branch, from a repository that is already on disk.
package list

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"git-suite/git-list-tags/internal/codes"
)

const maxTagDepth = 10

// Options is one stateless listing request.
type Options struct {
	Path   string
	Branch string
}

// Result is the success document.
type Result struct {
	Status   string   `json:"status"`
	Path     string   `json:"path"`
	URL      string   `json:"url"`
	Branches []Branch `json:"branches"`
}

// Branch is one local branch and the tags whose commits it contains.
type Branch struct {
	Branch string `json:"branch"`
	Tags   []Tag  `json:"tags"`
}

// Tag is one tag ref in the repository.
// An annotated tag reports its tagger. A lightweight tag has no tagger, so
// tagger_name, tagger_email, and when come from the tagged commit's committer.
// signed reports that the tag object carries a signature. The signature is
// not verified.
type Tag struct {
	Name        string `json:"name"`
	CommitID    string `json:"commit_id"`
	When        string `json:"when"`
	TaggerName  string `json:"tagger_name"`
	TaggerEmail string `json:"tagger_email"`
	Annotated   bool   `json:"annotated"`
	Signed      bool   `json:"signed"`
}

// Check validates a request without opening the repository.
func Check(opt Options) error {
	if strings.TrimSpace(opt.Path) == "" {
		return codes.New(codes.MissingValue, "path is required")
	}
	return validateBranch(opt.Branch)
}

// List returns the tags contained in the repository at opt.Path.
//
// Every tag ref in the repository must resolve to an object that exists
// there and peels to a commit. A tag is listed under a branch when that
// commit is reachable from the branch tip.
func List(opt Options) (Result, error) {
	if err := Check(opt); err != nil {
		return Result{}, err
	}
	dest, err := resolvePath(opt.Path)
	if err != nil {
		return Result{}, err
	}
	if err := requireRepository(dest); err != nil {
		return Result{}, err
	}
	repo, err := git.PlainOpen(dest)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return Result{}, codes.New(codes.NotRepository, "folder is not a git repository")
		}
		return Result{}, codes.New(codes.RepositoryUnreadable, "cannot read repository: %v", err)
	}
	rawURL, err := repositoryURL(repo)
	if err != nil {
		return Result{}, err
	}
	refs, err := selectBranches(repo, opt.Branch)
	if err != nil {
		return Result{}, err
	}
	tags, err := loadTags(repo)
	if err != nil {
		return Result{}, err
	}
	branches := make([]Branch, 0, len(refs))
	if len(tags) == 0 {
		for _, ref := range refs {
			branches = append(branches, Branch{Branch: ref.name, Tags: []Tag{}})
		}
		return Result{Status: "ok", Path: dest, URL: rawURL, Branches: branches}, nil
	}
	for _, ref := range refs {
		branch, err := listBranch(repo, ref, tags)
		if err != nil {
			return Result{}, err
		}
		branches = append(branches, branch)
	}
	return Result{Status: "ok", Path: dest, URL: rawURL, Branches: branches}, nil
}

func repositoryURL(repo *git.Repository) (string, error) {
	cfg, err := repo.Storer.Config()
	if err != nil {
		return "", codes.New(codes.RepositoryUnreadable, "cannot read repository config: %v", err)
	}
	remote, ok := cfg.Remotes["origin"]
	if !ok || remote == nil || len(remote.URLs) == 0 || strings.TrimSpace(remote.URLs[0]) == "" {
		return "", codes.New(codes.NoRemoteURL, "repository has no origin url")
	}
	return strings.TrimSpace(remote.URLs[0]), nil
}

func resolvePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", codes.New(codes.InvalidPath, "path contains a NUL byte")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", codes.New(codes.InvalidPath, "cannot resolve path: %v", err)
	}
	abs = filepath.Clean(abs)
	if abs == string(filepath.Separator) {
		return "", codes.New(codes.InvalidPath, "refusing to use the filesystem root")
	}
	return abs, nil
}

func requireRepository(dest string) error {
	info, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return codes.New(codes.FolderMissing, "folder does not exist")
	}
	if err != nil {
		return codes.New(codes.RepositoryUnreadable, "cannot inspect path: %v", err)
	}
	if !info.IsDir() {
		return codes.New(codes.NotDirectory, "path exists and is not a directory")
	}
	return nil
}

func validateBranch(name string) error {
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") ||
		strings.HasSuffix(name, ".lock") || strings.Contains(name, "..") ||
		strings.Contains(name, "@{") || strings.Contains(name, "//") {
		return codes.New(codes.InvalidBranch, "branch name %q is invalid", name)
	}
	for _, r := range name {
		if r < 32 || r == 127 || strings.ContainsRune("~^:?*[\\ ", r) {
			return codes.New(codes.InvalidBranch, "branch name %q is invalid", name)
		}
	}
	return nil
}

type branchRef struct {
	name string
	hash plumbing.Hash
}

func selectBranches(repo *git.Repository, name string) ([]branchRef, error) {
	if name != "" {
		ref, err := repo.Reference(plumbing.NewBranchReferenceName(name), true)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, codes.New(codes.BranchNotFound, "branch %q was not found", name)
		}
		if err != nil {
			return nil, codes.New(codes.RepositoryUnreadable, "cannot read branch %q: %v", name, err)
		}
		return []branchRef{{name: ref.Name().Short(), hash: ref.Hash()}}, nil
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list branches: %v", err)
	}
	defer iter.Close()
	var refs []branchRef
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		refs = append(refs, branchRef{name: ref.Name().Short(), hash: ref.Hash()})
		return nil
	})
	if err != nil {
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list branches: %v", err)
	}
	if len(refs) == 0 {
		return nil, codes.New(codes.NoBranches, "repository has no branches")
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].name < refs[j].name })
	return refs, nil
}

type resolvedTag struct {
	name      string
	commit    plumbing.Hash
	when      string
	whoName   string
	whoEmail  string
	annotated bool
	signed    bool
}

func loadTags(repo *git.Repository) ([]resolvedTag, error) {
	iter, err := repo.Tags()
	if err != nil {
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list tags: %v", err)
	}
	defer iter.Close()
	var tags []resolvedTag
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		tag, err := resolveTag(repo, ref.Name().Short(), ref.Hash())
		if err != nil {
			return err
		}
		tags = append(tags, tag)
		return nil
	})
	if err != nil {
		var ce *codes.Error
		if errors.As(err, &ce) {
			return nil, ce
		}
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list tags: %v", err)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].name < tags[j].name })
	return tags, nil
}

func resolveTag(repo *git.Repository, name string, hash plumbing.Hash) (resolvedTag, error) {
	var annotated *object.Tag
	current := hash
	for depth := 0; depth < maxTagDepth; depth++ {
		obj, err := repo.Object(plumbing.AnyObject, current)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return resolvedTag{}, codes.New(codes.TagUnreadable, "tag %q points at %s, which does not exist in the repository", name, current.String())
		}
		if err != nil {
			return resolvedTag{}, codes.New(codes.TagUnreadable, "cannot read tag %q: %v", name, err)
		}
		switch typed := obj.(type) {
		case *object.Commit:
			return describeTag(name, typed, annotated), nil
		case *object.Tag:
			if annotated == nil {
				annotated = typed
			}
			if typed.Target == plumbing.ZeroHash {
				return resolvedTag{}, codes.New(codes.TagUnreadable, "tag %q has no target", name)
			}
			current = typed.Target
		default:
			return resolvedTag{}, codes.New(codes.TagNotCommit, "tag %q does not point at a commit", name)
		}
	}
	return resolvedTag{}, codes.New(codes.TagUnreadable, "tag %q is nested too deeply", name)
}

func describeTag(name string, commit *object.Commit, annotated *object.Tag) resolvedTag {
	who := commit.Committer
	signed := false
	isAnnotated := annotated != nil
	if isAnnotated {
		who = annotated.Tagger
		signed = strings.TrimSpace(annotated.PGPSignature) != ""
	}
	when := ""
	if !who.When.IsZero() {
		when = who.When.UTC().Format(time.RFC3339)
	}
	return resolvedTag{
		name:      name,
		commit:    commit.Hash,
		when:      when,
		whoName:   clipText(who.Name),
		whoEmail:  clipText(who.Email),
		annotated: isAnnotated,
		signed:    signed,
	}
}

func listBranch(repo *git.Repository, ref branchRef, tags []resolvedTag) (Branch, error) {
	out := Branch{Branch: ref.name, Tags: []Tag{}}
	reachable, err := commitsReachableFrom(repo, ref.hash)
	if err != nil {
		return Branch{}, err
	}
	for _, tag := range tags {
		if _, ok := reachable[tag.commit]; !ok {
			continue
		}
		out.Tags = append(out.Tags, Tag{
			Name:        tag.name,
			CommitID:    shortCommit(tag.commit.String()),
			When:        tag.when,
			TaggerName:  tag.whoName,
			TaggerEmail: tag.whoEmail,
			Annotated:   tag.annotated,
			Signed:      tag.signed,
		})
	}
	return out, nil
}

func commitsReachableFrom(repo *git.Repository, tip plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	seen := map[plumbing.Hash]struct{}{}
	pending := []plumbing.Hash{tip}
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[hash]; ok {
			continue
		}
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return nil, codes.New(codes.RepositoryUnreadable, "cannot read commit %s: %v", hash.String(), err)
		}
		seen[hash] = struct{}{}
		pending = append(pending, commit.ParentHashes...)
	}
	return seen, nil
}

func shortCommit(id string) string {
	const n = 8
	if len(id) <= n {
		return id
	}
	return id[:n]
}

func clipText(s string) string {
	const max = 1024
	if len(s) <= max {
		return s
	}
	cut := max - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut < 1 {
		return "..."
	}
	return s[:cut] + "..."
}
