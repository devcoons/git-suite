// Package syncer clones a repository or refreshes a directory that already
// contains that repository.
package syncer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"git-suite/git-sync/internal/codes"
)

const (
	// ActionCloned means the directory was created from a fresh clone.
	ActionCloned = "cloned"
	// ActionUpdated means an existing checkout was changed to match the remote.
	ActionUpdated = "updated"
	// ActionUnchanged means the checkout already matched the remote.
	ActionUnchanged = "unchanged"
)

// Options is one stateless sync request.
type Options struct {
	URL                 string
	Path                string
	Branch              string
	Username            string
	Password            string
	SSHKey              []byte
	SSHPassphrase       string
	KnownHosts          string
	InsecureSkipHostKey bool
	Replace             bool
	Progress            io.Writer
}

// Result is the success document.
type Result struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	URL    string `json:"url"`
	Action string `json:"action"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

type plan struct {
	loc  Location
	dest string
	auth transport.AuthMethod
	opt  Options
}

// Check validates a request without touching the destination.
func Check(opt Options) error {
	_, err := prepare(opt)
	return err
}

// Sync makes dest match the requested repository.
//
// An existing checkout of the same repository is fetched and hard-reset to
// the remote branch. Local commits and uncommitted files are discarded.
// Anything else is cloned into a temporary directory and swapped into place
// only after the clone succeeds, so a failed attempt leaves the previous
// directory as it was.
func Sync(ctx context.Context, opt Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p, err := prepare(opt)
	if err != nil {
		return Result{}, err
	}

	kind, openErr := inspect(p.dest, p.loc)
	if openErr != nil {
		return Result{}, openErr
	}
	switch kind {
	case kindMissing, kindEmpty, kindBroken:
		return cloneNew(ctx, p, ActionCloned)
	case kindNotDir:
		return Result{}, codes.New(codes.NotDirectory, "path exists and is not a directory")
	case kindForeign:
		if !p.opt.Replace {
			return Result{}, codes.New(codes.RefusingReplace, "directory is not a git repository and is not empty; pass replace to overwrite it")
		}
		return cloneNew(ctx, p, ActionCloned)
	case kindSame:
		result, err := update(ctx, p)
		if err == nil {
			return result, nil
		}
		if definitive(err) {
			return Result{}, err
		}
		cloned, cerr := cloneNew(ctx, p, ActionCloned)
		if cerr != nil {
			var ce *codes.Error
			if errors.As(cerr, &ce) && (ce.Code == codes.AuthFailed || ce.Code == codes.BranchNotFound || ce.Code == codes.BadCredentials) {
				return Result{}, cerr
			}
			return Result{}, codes.New(codes.UpdateFailed, "update failed (%s); clone failed (%s)", err.Error(), cerr.Error())
		}
		return cloned, nil
	case kindDifferent:
		return cloneNew(ctx, p, ActionCloned)
	default:
		return Result{}, codes.New(codes.Internal, "unhandled destination state")
	}
}

func prepare(opt Options) (plan, error) {
	if strings.TrimSpace(opt.URL) == "" {
		return plan{}, codes.New(codes.MissingValue, "url is required")
	}
	if strings.TrimSpace(opt.Path) == "" {
		return plan{}, codes.New(codes.MissingValue, "path is required")
	}
	loc, err := ParseLocation(opt.URL)
	if err != nil {
		return plan{}, err
	}
	if err := ValidateBranch(opt.Branch); err != nil {
		return plan{}, err
	}
	dest, err := resolveDest(opt.Path)
	if err != nil {
		return plan{}, err
	}
	if err := rejectOverlap(loc, dest); err != nil {
		return plan{}, err
	}
	auth, err := buildAuth(loc, opt)
	if err != nil {
		return plan{}, err
	}
	opt.URL = loc.URL
	return plan{loc: loc, dest: dest, auth: auth, opt: opt}, nil
}

func resolveDest(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", codes.New(codes.MissingValue, "path is required")
	}
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

func rejectOverlap(loc Location, dest string) error {
	if loc.Scheme != "file" {
		return nil
	}
	if nested(loc.Identity.Path, dest) || nested(dest, loc.Identity.Path) {
		return codes.New(codes.InvalidPath, "destination must be outside the source repository")
	}
	return nil
}

func nested(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

type destKind int

const (
	kindMissing destKind = iota
	kindEmpty
	kindNotDir
	kindForeign
	kindBroken
	kindSame
	kindDifferent
)

func inspect(dest string, loc Location) (destKind, error) {
	info, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return kindMissing, nil
	}
	if err != nil {
		return 0, codes.New(codes.Destination, "cannot inspect path: %v", err)
	}
	if !info.IsDir() {
		return kindNotDir, nil
	}
	repo, err := git.PlainOpen(dest)
	if err != nil {
		empty, emptyErr := isEmpty(dest)
		if emptyErr != nil {
			return 0, emptyErr
		}
		if empty {
			return kindEmpty, nil
		}
		if _, statErr := os.Lstat(filepath.Join(dest, ".git")); statErr == nil {
			return kindBroken, nil
		}
		return kindForeign, nil
	}
	current, err := originURL(repo)
	if err != nil {
		return kindDifferent, nil
	}
	existing, err := ParseLocation(current)
	if err != nil {
		return kindDifferent, nil
	}
	if existing.Identity.Equal(loc.Identity) {
		return kindSame, nil
	}
	return kindDifferent, nil
}

func isEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, codes.New(codes.Destination, "cannot read directory: %v", err)
	}
	defer f.Close()
	entries, err := f.Readdir(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, codes.New(codes.Destination, "cannot read directory: %v", err)
	}
	return len(entries) == 0, nil
}

func originURL(repo *git.Repository) (string, error) {
	cfg, err := localConfig(repo)
	if err != nil {
		return "", err
	}
	remote, ok := cfg.Remotes["origin"]
	if !ok || len(remote.URLs) == 0 {
		return "", errors.New("origin has no URL")
	}
	return remote.URLs[0], nil
}

func cloneNew(ctx context.Context, p plan, action string) (Result, error) {
	parent := filepath.Dir(p.dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, codes.New(codes.Destination, "cannot create parent directory: %v", err)
	}
	tmp, err := os.MkdirTemp(parent, ".git-sync-*")
	if err != nil {
		return Result{}, codes.New(codes.Destination, "cannot create a temporary directory: %v", err)
	}
	defer os.RemoveAll(tmp)

	if err := cloneDir(ctx, p, tmp); err != nil {
		return Result{}, err
	}
	if err := install(tmp, p.dest); err != nil {
		return Result{}, err
	}
	return readResult(p.dest, p.loc.URL, action)
}

func cloneDir(ctx context.Context, p plan, dest string) error {
	opts := &git.CloneOptions{
		URL:      p.loc.URL,
		Auth:     p.auth,
		Progress: p.opt.Progress,
		Tags:     git.AllTags,
	}
	if p.opt.Branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(p.opt.Branch)
	}
	_, err := git.PlainCloneContext(ctx, dest, false, opts)
	if err != nil {
		return mapGit(err, codes.CloneFailed, p.opt)
	}
	return nil
}

// install publishes a finished clone. The previous directory is removed only
// after the new directory is in place.
func install(tmp, dest string) error {
	if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(tmp, dest); err != nil {
			return codes.New(codes.ReplaceFailed, "cannot move clone into place: %v", err)
		}
		return nil
	} else if err != nil {
		return codes.New(codes.ReplaceFailed, "cannot inspect destination: %v", err)
	}

	backup := filepath.Join(filepath.Dir(dest), ".git-sync-previous-"+randomToken())
	if err := os.Rename(dest, backup); err != nil {
		return codes.New(codes.ReplaceFailed, "cannot move the existing directory aside: %v", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		if rbErr := os.Rename(backup, dest); rbErr != nil {
			return codes.New(codes.ReplaceFailed, "cannot move clone into place (%v); previous directory remains at %s", err, backup)
		}
		return codes.New(codes.ReplaceFailed, "cannot move clone into place: %v", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return codes.New(codes.ReplaceFailed, "clone is in place, but the previous directory remains at %s: %v", backup, err)
	}
	return nil
}

func update(ctx context.Context, p plan) (Result, error) {
	repo, err := git.PlainOpen(p.dest)
	if err != nil {
		return Result{}, mapGit(err, codes.RepositoryUnreadable, p.opt)
	}
	urlChanged, err := setOrigin(repo, p.loc.URL)
	if err != nil {
		return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
	}
	branch := p.opt.Branch
	if branch == "" {
		branch, err = defaultBranch(ctx, repo, p.auth)
		if err != nil {
			return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
		}
	}
	fetchErr := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Auth:       p.auth,
		Force:      true,
		Prune:      true,
		Tags:       git.AllTags,
		Progress:   p.opt.Progress,
	})
	if fetchErr != nil && !errors.Is(fetchErr, git.NoErrAlreadyUpToDate) {
		return Result{}, mapGit(fetchErr, codes.UpdateFailed, p.opt)
	}
	hash, err := remoteBranchHash(repo, branch)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return Result{}, mapGit(err, codes.BranchNotFound, p.opt)
		}
		return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
	}
	matched, err := matches(repo, branch, hash)
	if err != nil {
		return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
	}
	action := ActionUnchanged
	if !matched {
		if err := pointBranch(repo, branch, hash); err != nil {
			return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
		}
		if err := alignWorktree(repo, branch, hash); err != nil {
			return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
		}
		action = ActionUpdated
	}
	if urlChanged {
		action = ActionUpdated
	}
	if action == ActionUpdated {
		if err := setTracking(repo, branch); err != nil {
			return Result{}, mapGit(err, codes.UpdateFailed, p.opt)
		}
	}
	return Result{
		Status: "ok",
		Path:   p.dest,
		URL:    p.loc.URL,
		Action: action,
		Branch: branch,
		Commit: shortCommit(hash.String()),
	}, nil
}

func setOrigin(repo *git.Repository, rawURL string) (bool, error) {
	cfg, err := localConfig(repo)
	if err != nil {
		return false, err
	}
	if cfg.Remotes == nil {
		cfg.Remotes = map[string]*config.RemoteConfig{}
	}
	remote := cfg.Remotes["origin"]
	if remote == nil {
		cfg.Remotes["origin"] = &config.RemoteConfig{
			Name:  "origin",
			URLs:  []string{rawURL},
			Fetch: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		}
		return true, repo.Storer.SetConfig(cfg)
	}
	changed := len(remote.URLs) != 1 || remote.URLs[0] != rawURL
	if !changed && len(remote.Fetch) > 0 {
		return false, nil
	}
	remote.URLs = []string{rawURL}
	if len(remote.Fetch) == 0 {
		remote.Fetch = []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}
		changed = true
	}
	if err := repo.Storer.SetConfig(cfg); err != nil {
		return false, err
	}
	return changed, nil
}

func defaultBranch(ctx context.Context, repo *git.Repository, auth transport.AuthMethod) (string, error) {
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", err
	}
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return "", err
	}
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD && ref.Target().IsBranch() {
			return ref.Target().Short(), nil
		}
	}
	for _, name := range []string{"main", "master"} {
		want := plumbing.NewBranchReferenceName(name)
		for _, ref := range refs {
			if ref.Name() == want {
				return name, nil
			}
		}
	}
	return "", codes.New(codes.BranchNotFound, "remote did not advertise a default branch; pass a branch name")
}

func remoteBranchHash(repo *git.Repository, branch string) (plumbing.Hash, error) {
	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if ref.Hash() == plumbing.ZeroHash {
		return plumbing.ZeroHash, plumbing.ErrReferenceNotFound
	}
	return ref.Hash(), nil
}

func matches(repo *git.Repository, branch string, hash plumbing.Hash) (bool, error) {
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if head.Name() != plumbing.NewBranchReferenceName(branch) || head.Hash() != hash {
		return false, nil
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return false, err
	}
	status, err := worktree.Status()
	if err != nil {
		return false, err
	}
	return status.IsClean(), nil
}

func pointBranch(repo *git.Repository, branch string, hash plumbing.Hash) error {
	return repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), hash))
}

func alignWorktree(repo *git.Repository, branch string, hash plumbing.Hash) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Force:  true,
	}); err != nil {
		return err
	}
	if err := worktree.Reset(&git.ResetOptions{Commit: hash, Mode: git.HardReset}); err != nil {
		return err
	}
	return worktree.Clean(&git.CleanOptions{Dir: true})
}

func setTracking(repo *git.Repository, branch string) error {
	cfg, err := localConfig(repo)
	if err != nil {
		return err
	}
	if cfg.Branches == nil {
		cfg.Branches = map[string]*config.Branch{}
	}
	cfg.Branches[branch] = &config.Branch{
		Name:   branch,
		Remote: "origin",
		Merge:  plumbing.NewBranchReferenceName(branch),
	}
	return repo.Storer.SetConfig(cfg)
}

func localConfig(repo *git.Repository) (*config.Config, error) {
	return repo.Storer.Config()
}

func readResult(dest, rawURL, action string) (Result, error) {
	repo, err := git.PlainOpen(dest)
	if err != nil {
		return Result{}, codes.New(codes.RepositoryUnreadable, "repository cannot be read after clone: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		return Result{}, codes.New(codes.RepositoryUnreadable, "HEAD cannot be read after clone: %v", err)
	}
	branch := ""
	if head.Name().IsBranch() {
		branch = head.Name().Short()
	}
	return Result{
		Status: "ok",
		Path:   dest,
		URL:    rawURL,
		Action: action,
		Branch: branch,
		Commit: shortCommit(head.Hash().String()),
	}, nil
}

func mapGit(err error, fallback int, opt Options) error {
	if err == nil {
		return nil
	}
	var ce *codes.Error
	if errors.As(err, &ce) {
		return ce
	}
	code := fallback
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired),
		errors.Is(err, transport.ErrAuthorizationFailed),
		strings.Contains(low, "authentication required"),
		strings.Contains(low, "authorization failed"):
		code = codes.AuthFailed
	case errors.Is(err, plumbing.ErrReferenceNotFound),
		strings.Contains(low, "couldn't find remote ref"),
		strings.Contains(low, "reference not found"):
		code = codes.BranchNotFound
	}
	return codes.New(code, "%s", redact(msg, opt.Password, opt.SSHPassphrase))
}

func definitive(err error) bool {
	var ce *codes.Error
	if !errors.As(err, &ce) {
		return false
	}
	switch ce.Code {
	case codes.AuthFailed, codes.BranchNotFound, codes.BadCredentials, codes.HostKeyConfig, codes.SSHKey:
		return true
	default:
		return false
	}
}

func randomToken() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(buf)
}

func shortCommit(id string) string {
	const n = 8
	if len(id) <= n {
		return id
	}
	return id[:n]
}
