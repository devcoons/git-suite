package syncer

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"git-suite/git-sync/internal/codes"
)

// Identity is the host and repository path used to decide that two URLs
// name the same repository. Scheme and embedded user are ignored, and a
// trailing ".git" on a network path is ignored.
type Identity struct {
	Host string
	Path string
}

// Equal reports whether both values name the same repository.
func (id Identity) Equal(other Identity) bool {
	return id.Host == other.Host && id.Path == other.Path
}

// Location is a parsed repository URL.
type Location struct {
	Scheme   string
	User     string
	URL      string
	Identity Identity
}

var scpURL = regexp.MustCompile(`^([^@/\s]+)@([^:/\s]+):(.+)$`)

// ParseLocation accepts http, https, ssh, scp-style SSH, and absolute file URLs.
func ParseLocation(raw string) (Location, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Location{}, codes.New(codes.InvalidURL, "repository URL is required")
	}
	if strings.ContainsRune(raw, 0) || strings.ContainsAny(raw, "\r\n") {
		return Location{}, codes.New(codes.InvalidURL, "repository URL contains a control character")
	}
	if !strings.Contains(raw, "://") {
		return parseSCP(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Location{}, codes.New(codes.InvalidURL, "repository URL is invalid")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Location{}, codes.New(codes.InvalidURL, "repository URL must not contain a query or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		return parseFile(u)
	case "http", "https", "ssh":
		return parseNetwork(u)
	default:
		return Location{}, codes.New(codes.InvalidURL, "unsupported repository URL scheme %q", u.Scheme)
	}
}

func parseSCP(raw string) (Location, error) {
	m := scpURL.FindStringSubmatch(raw)
	if m == nil {
		return Location{}, codes.New(codes.InvalidURL, "repository URL is invalid")
	}
	repoPath, err := normalizeRepoPath(m[3])
	if err != nil {
		return Location{}, err
	}
	host := strings.ToLower(m[2])
	return Location{
		Scheme:   "ssh",
		User:     m[1],
		URL:      raw,
		Identity: Identity{Host: host, Path: repoPath},
	}, nil
}

func parseFile(u *url.URL) (Location, error) {
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			return Location{}, codes.New(codes.InvalidURL, "put the password in the password field")
		}
		return Location{}, codes.New(codes.InvalidURL, "file URLs do not accept a user")
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "", "localhost", "127.0.0.1":
		host = ""
	default:
		return Location{}, codes.New(codes.InvalidURL, "file URLs must point at the local machine")
	}
	if u.Path == "" {
		return Location{}, codes.New(codes.InvalidURL, "file URL is missing a path")
	}
	abs := filepath.Clean(u.Path)
	if !filepath.IsAbs(abs) {
		return Location{}, codes.New(codes.InvalidURL, "file URL path must be absolute")
	}
	canonical := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	return Location{
		Scheme:   "file",
		URL:      canonical,
		Identity: Identity{Path: abs},
	}, nil
}

func parseNetwork(u *url.URL) (Location, error) {
	user := ""
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			return Location{}, codes.New(codes.InvalidURL, "put the password in the password field")
		}
		user = u.User.Username()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Location{}, codes.New(codes.InvalidURL, "repository URL is missing a host")
	}
	if port := u.Port(); port != "" && !defaultPort(u.Scheme, port) {
		host = host + ":" + port
	}
	repoPath, err := normalizeRepoPath(u.Path)
	if err != nil {
		return Location{}, err
	}
	scheme := strings.ToLower(u.Scheme)
	return Location{
		Scheme:   scheme,
		User:     user,
		URL:      u.String(),
		Identity: Identity{Host: host, Path: repoPath},
	}, nil
}

func normalizeRepoPath(p string) (string, error) {
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	if p == "" {
		return "", codes.New(codes.InvalidURL, "repository URL is missing a path")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", codes.New(codes.InvalidURL, "repository URL path is invalid")
	}
	return cleaned, nil
}

func defaultPort(scheme, port string) bool {
	switch strings.ToLower(scheme) {
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	case "ssh":
		return port == "22"
	default:
		return false
	}
}

// ValidateBranch accepts an empty name, which means the remote default branch.
func ValidateBranch(name string) error {
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
