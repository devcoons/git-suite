// Package info describes local branches in a repository that is already on disk.
package info

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

	"git-suite/git-get-branch-info/internal/codes"
)

// Options is one stateless request.
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

// Branch is the history of one local branch.
// Times are the committer times, in UTC RFC 3339, because that is when the
// commit was created. The first commit is the earliest root commit reachable
// from the tip. root_count is greater than one when the history has more
// than one parentless commit.
type Branch struct {
	Branch              string `json:"branch"`
	CheckedOut          bool   `json:"checked_out"`
	CommitCount         int    `json:"commit_count"`
	RootCount           int    `json:"root_count"`
	FirstCommitID       string `json:"first_commit_id"`
	FirstCommitAt       string `json:"first_commit_at"`
	FirstCommitterName  string `json:"first_committer_name"`
	FirstCommitterEmail string `json:"first_committer_email"`
	LastCommitID        string `json:"last_commit_id"`
	LastCommitAt        string `json:"last_commit_at"`
	LastCommitterName   string `json:"last_committer_name"`
	LastCommitterEmail  string `json:"last_committer_email"`
	LastCommitSubject   string `json:"last_commit_subject"`
}

// Check validates a request without opening the repository.
func Check(opt Options) error {
	if strings.TrimSpace(opt.Path) == "" {
		return codes.New(codes.MissingValue, "path is required")
	}
	return validateBranch(opt.Branch)
}

// Collect describes the requested branch, or every local branch.
func Collect(opt Options) (Result, error) {
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
	current, err := checkedOutBranch(repo)
	if err != nil {
		return Result{}, err
	}
	refs, err := selectBranches(repo, opt.Branch)
	if err != nil {
		return Result{}, err
	}
	summaries := map[plumbing.Hash]Branch{}
	branches := make([]Branch, 0, len(refs))
	for _, ref := range refs {
		if summary, ok := summaries[ref.hash]; ok {
			summary.Branch = ref.name
			summary.CheckedOut = ref.name == current
			branches = append(branches, summary)
			continue
		}
		branch, err := describe(repo, ref, current)
		if err != nil {
			return Result{}, err
		}
		summaries[ref.hash] = branch
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

func checkedOutBranch(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", nil
	}
	if err != nil {
		return "", codes.New(codes.RepositoryUnreadable, "cannot read HEAD: %v", err)
	}
	if head.Name().IsBranch() {
		return head.Name().Short(), nil
	}
	return "", nil
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

func describe(repo *git.Repository, ref branchRef, current string) (Branch, error) {
	tip, err := repo.CommitObject(ref.hash)
	if err != nil {
		return Branch{}, codes.New(codes.HistoryUnreadable, "cannot read the tip of branch %q: %v", ref.name, err)
	}
	seen := map[plumbing.Hash]struct{}{}
	pending := []plumbing.Hash{ref.hash}
	var roots []*object.Commit
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[hash]; ok {
			continue
		}
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return Branch{}, codes.New(codes.HistoryUnreadable, "cannot read commit %s on branch %q: %v", hash.String(), ref.name, err)
		}
		seen[hash] = struct{}{}
		if len(commit.ParentHashes) == 0 {
			roots = append(roots, commit)
		}
		pending = append(pending, commit.ParentHashes...)
	}
	if len(roots) == 0 {
		return Branch{}, codes.New(codes.HistoryUnreadable, "branch %q has no root commit", ref.name)
	}
	first := earliest(roots)
	return Branch{
		Branch:              ref.name,
		CheckedOut:          ref.name == current,
		CommitCount:         len(seen),
		RootCount:           len(roots),
		FirstCommitID:       shortCommit(first.Hash.String()),
		FirstCommitAt:       formatTime(first.Committer.When),
		FirstCommitterName:  clipText(first.Committer.Name),
		FirstCommitterEmail: clipText(first.Committer.Email),
		LastCommitID:        shortCommit(tip.Hash.String()),
		LastCommitAt:        formatTime(tip.Committer.When),
		LastCommitterName:   clipText(tip.Committer.Name),
		LastCommitterEmail:  clipText(tip.Committer.Email),
		LastCommitSubject:   subject(tip.Message),
	}, nil
}

func shortCommit(id string) string {
	const n = 8
	if len(id) <= n {
		return id
	}
	return id[:n]
}

func earliest(roots []*object.Commit) *object.Commit {
	best := roots[0]
	for _, commit := range roots[1:] {
		if commit.Committer.When.Before(best.Committer.When) {
			best = commit
			continue
		}
		if commit.Committer.When.Equal(best.Committer.When) && commit.Hash.String() < best.Hash.String() {
			best = commit
		}
	}
	return best
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func subject(message string) string {
	message = strings.TrimSpace(message)
	if i := strings.IndexAny(message, "\r\n"); i >= 0 {
		message = message[:i]
	}
	return clipText(strings.TrimSpace(message))
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
