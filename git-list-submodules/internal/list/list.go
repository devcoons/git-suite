// Package list reads the submodules recorded by one local branch, or by
// every local branch, from a repository that is already on disk.
package list

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	formatcfg "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"git-suite/git-list-submodules/internal/codes"
)

const maxModulesBytes = 1 << 20

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

// Branch is one local branch and the submodules its tip commit records.
type Branch struct {
	Branch     string      `json:"branch"`
	Submodules []Submodule `json:"submodules"`
}

// Submodule is one gitlink recorded by a branch.
// The URL and commit come from that branch's commit. The submodule
// repository is not cloned.
type Submodule struct {
	URL      string `json:"url"`
	Path     string `json:"path"`
	CommitID string `json:"commit_id"`
}

// Check validates a request without opening the repository.
func Check(opt Options) error {
	if strings.TrimSpace(opt.Path) == "" {
		return codes.New(codes.MissingValue, "path is required")
	}
	return validateBranch(opt.Branch)
}

// List returns the submodules recorded in the repository at opt.Path.
//
// A submodule exists when that branch's commit contains a gitlink at the
// path named in .gitmodules. The working tree does not have to contain a
// checkout of the submodule, and this command does not clone one.
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
	branches := make([]Branch, 0, len(refs))
	for _, ref := range refs {
		branch, err := listBranch(repo, ref)
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

func selectBranches(repo *git.Repository, name string) ([]*plumbing.Reference, error) {
	if name != "" {
		ref, err := repo.Reference(plumbing.NewBranchReferenceName(name), true)
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, codes.New(codes.BranchNotFound, "branch %q was not found", name)
		}
		if err != nil {
			return nil, codes.New(codes.RepositoryUnreadable, "cannot read branch %q: %v", name, err)
		}
		return []*plumbing.Reference{ref}, nil
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list branches: %v", err)
	}
	defer iter.Close()
	var refs []*plumbing.Reference
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		refs = append(refs, ref)
		return nil
	})
	if err != nil {
		return nil, codes.New(codes.RepositoryUnreadable, "cannot list branches: %v", err)
	}
	if len(refs) == 0 {
		return nil, codes.New(codes.NoBranches, "repository has no branches")
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Name().Short() < refs[j].Name().Short()
	})
	return refs, nil
}

func listBranch(repo *git.Repository, ref *plumbing.Reference) (Branch, error) {
	name := ref.Name().Short()
	out := Branch{Branch: name, Submodules: []Submodule{}}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return Branch{}, codes.New(codes.RepositoryUnreadable, "cannot read branch %q: %v", name, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return Branch{}, codes.New(codes.RepositoryUnreadable, "cannot read tree of branch %q: %v", name, err)
	}
	modules, err := readModules(commit, name)
	if err != nil {
		return Branch{}, err
	}
	links, err := readGitlinks(tree, name)
	if err != nil {
		return Branch{}, err
	}
	submodules, err := matchSubmodules(tree, name, modules, links)
	if err != nil {
		return Branch{}, err
	}
	out.Submodules = submodules
	return out, nil
}

func readModules(commit *object.Commit, branch string) (*config.Modules, error) {
	file, err := commit.File(".gitmodules")
	if errors.Is(err, object.ErrFileNotFound) {
		return config.NewModules(), nil
	}
	if err != nil {
		return nil, codes.New(codes.ModulesInvalid, "cannot read .gitmodules on branch %q: %v", branch, err)
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, codes.New(codes.ModulesInvalid, "cannot read .gitmodules on branch %q: %v", branch, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxModulesBytes+1))
	if err != nil {
		return nil, codes.New(codes.ModulesInvalid, "cannot read .gitmodules on branch %q: %v", branch, err)
	}
	if len(data) > maxModulesBytes {
		return nil, codes.New(codes.ModulesInvalid, ".gitmodules on branch %q exceeds 1MiB", branch)
	}
	modules := config.NewModules()
	if err := modules.Unmarshal(data); err != nil {
		return nil, codes.New(codes.ModulesInvalid, "invalid .gitmodules on branch %q: %v", branch, err)
	}
	if err := rejectDroppedModules(data, modules, branch); err != nil {
		return nil, err
	}
	return modules, nil
}

// go-git drops submodule entries whose name or path fails its safety checks.
// Those entries are still in the file, so a silent drop would hide a bad record.
func rejectDroppedModules(data []byte, modules *config.Modules, branch string) error {
	raw := formatcfg.New()
	if err := formatcfg.NewDecoder(bytes.NewReader(data)).Decode(raw); err != nil {
		return codes.New(codes.ModulesInvalid, "invalid .gitmodules on branch %q: %v", branch, err)
	}
	if got, want := len(modules.Submodules), len(raw.Section("submodule").Subsections); got != want {
		return codes.New(codes.ModulesInvalid, ".gitmodules on branch %q contains a submodule entry that cannot be used", branch)
	}
	return nil
}

func readGitlinks(tree *object.Tree, branch string) (map[string]plumbing.Hash, error) {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	links := map[string]plumbing.Hash{}
	for {
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return links, nil
		}
		if err != nil {
			return nil, codes.New(codes.RepositoryUnreadable, "cannot walk tree of branch %q: %v", branch, err)
		}
		if entry.Mode == filemode.Submodule {
			links[name] = entry.Hash
		}
	}
}

func matchSubmodules(tree *object.Tree, branch string, modules *config.Modules, links map[string]plumbing.Hash) ([]Submodule, error) {
	names := make([]string, 0, len(modules.Submodules))
	for name := range modules.Submodules {
		names = append(names, name)
	}
	sort.Strings(names)

	claimed := map[string]string{}
	items := make([]Submodule, 0, len(names))
	for _, name := range names {
		mod := modules.Submodules[name]
		if err := mod.Validate(); err != nil {
			return nil, mapModuleErr(err, branch, name)
		}
		repoPath, err := cleanRepoPath(mod.Path)
		if err != nil {
			return nil, codes.New(codes.SubmodulePath, "submodule %q on branch %q has an invalid path", name, branch)
		}
		if other, ok := claimed[repoPath]; ok {
			return nil, codes.New(codes.ModulesInvalid, "submodules %q and %q on branch %q use path %q", other, name, branch, repoPath)
		}
		hash, ok := links[repoPath]
		if !ok {
			return nil, missingSubmodule(tree, branch, name, repoPath)
		}
		if strings.TrimSpace(mod.URL) == "" {
			return nil, codes.New(codes.SubmoduleURL, "submodule %q on branch %q has no URL", name, branch)
		}
		claimed[repoPath] = name
		items = append(items, Submodule{
			URL:      mod.URL,
			Path:     repoPath,
			CommitID: shortCommit(hash.String()),
		})
	}

	var extra []string
	for repoPath := range links {
		if _, ok := claimed[repoPath]; !ok {
			extra = append(extra, repoPath)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, codes.New(codes.SubmoduleURL, "submodule path %q on branch %q has no URL in .gitmodules", extra[0], branch)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

func missingSubmodule(tree *object.Tree, branch, name, repoPath string) error {
	entry, err := tree.FindEntry(repoPath)
	if err != nil {
		if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrFileNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return codes.New(codes.SubmoduleMissing, "submodule %q path %q does not exist on branch %q", name, repoPath, branch)
		}
		return codes.New(codes.RepositoryUnreadable, "cannot look up path %q on branch %q: %v", repoPath, branch, err)
	}
	if entry.Mode != filemode.Submodule {
		return codes.New(codes.SubmoduleMissing, "submodule %q path %q on branch %q exists and is not a submodule", name, repoPath, branch)
	}
	return codes.New(codes.SubmoduleMissing, "submodule %q path %q does not exist on branch %q", name, repoPath, branch)
}

func cleanRepoPath(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
		return "", errors.New("invalid submodule path")
	}
	return p, nil
}

func shortCommit(id string) string {
	const n = 8
	if len(id) <= n {
		return id
	}
	return id[:n]
}

func mapModuleErr(err error, branch, name string) error {
	switch {
	case errors.Is(err, config.ErrModuleEmptyURL):
		return codes.New(codes.SubmoduleURL, "submodule %q on branch %q has no URL", name, branch)
	case errors.Is(err, config.ErrModuleEmptyPath), errors.Is(err, config.ErrModuleBadPath):
		return codes.New(codes.SubmodulePath, "submodule %q on branch %q has an invalid path", name, branch)
	default:
		return codes.New(codes.ModulesInvalid, "invalid .gitmodules entry %q on branch %q: %v", name, branch, err)
	}
}
