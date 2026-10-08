// Package request reads a git-sync invocation from flags or a JSON document.
package request

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"git-suite/git-sync/internal/codes"
	"git-suite/git-sync/internal/response"
	"git-suite/git-sync/internal/syncer"
)

const maxRequestBytes = 1 << 20

// ErrHelp is returned when the caller asked for usage text.
var ErrHelp = errors.New("help")

// ErrVersion is returned when the caller asked for the version.
var ErrVersion = errors.New("version")

// Usage is the standalone help text.
const Usage = `git-sync — clone a repository, or refresh a directory that already contains it.

Usage:
  git-sync --url <url> --path <dir> [options]
  git-sync --request <file|-> [--verbose]

Paths are resolved against the current working directory.
The process writes one packed JSON document to stdout and exits 0, or exits -1
(reported as 255) with a packed JSON error. --format bson writes one BSON
document instead. Secrets are safer in a --request document than in flags,
because flags are visible to other users on the machine.

Options:
  --url string                  repository URL (http, https, ssh, or file)
  --path string                 destination directory
  --branch string               branch to check out (default: remote HEAD)
  --username string             HTTP username
  --password string             HTTP password or token
  --ssh-key string              SSH private key file
  --ssh-passphrase string       passphrase for the SSH private key
  --ssh-known-hosts string      known_hosts file used to verify the SSH server
  --insecure-skip-host-key      accept any SSH host key
  --replace                     overwrite a non-empty directory that is not a git repository
  --request string              JSON request file, or - for stdin
  --format string               response encoding: json (default) or bson
  --verbose                     write transfer progress to stderr
  --version                     print the version and exit
`

type document struct {
	URL                 string `json:"url"`
	Path                string `json:"path"`
	Branch              string `json:"branch"`
	Username            string `json:"username"`
	Password            string `json:"password"`
	SSHPrivateKey       string `json:"ssh_private_key"`
	SSHPrivateKeyPath   string `json:"ssh_private_key_path"`
	SSHPassphrase       string `json:"ssh_passphrase"`
	SSHKnownHosts       string `json:"ssh_known_hosts"`
	InsecureSkipHostKey bool   `json:"insecure_skip_host_key"`
	Replace             bool   `json:"replace"`
	Format              string `json:"format"`
}

// Parse turns argv into a validated sync request and the response encoding.
func Parse(args []string, stdin io.Reader) (syncer.Options, response.Format, error) {
	format := response.JSON
	fs := flag.NewFlagSet("git-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	urlFlag := fs.String("url", "", "repository URL")
	pathFlag := fs.String("path", "", "destination directory")
	branchFlag := fs.String("branch", "", "branch to check out")
	username := fs.String("username", "", "HTTP username")
	password := fs.String("password", "", "HTTP password or token")
	sshKey := fs.String("ssh-key", "", "SSH private key file")
	sshPass := fs.String("ssh-passphrase", "", "SSH key passphrase")
	knownHosts := fs.String("ssh-known-hosts", "", "SSH known_hosts file")
	insecure := fs.Bool("insecure-skip-host-key", false, "skip SSH host key verification")
	replace := fs.Bool("replace", false, "overwrite a non-empty non-repository directory")
	requestPath := fs.String("request", "", "JSON request file or -")
	formatFlag := fs.String("format", "", "response encoding: json or bson")
	verbose := fs.Bool("verbose", false, "progress on stderr")
	version := fs.Bool("version", false, "print version")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return syncer.Options{}, format, ErrHelp
		}
		return syncer.Options{}, format, codes.New(codes.InvalidInvocation, "%s", err.Error())
	}
	if *version {
		return syncer.Options{}, format, ErrVersion
	}
	format, err := response.Select(visited(fs, "format"), *formatFlag, "")
	if err != nil {
		return syncer.Options{}, format, err
	}
	if fs.NArg() != 0 {
		return syncer.Options{}, format, codes.New(codes.InvalidInvocation, "unexpected argument %q", fs.Arg(0))
	}

	var opt syncer.Options
	var keyPath string
	if *requestPath != "" {
		if extra := conflicting(fs); len(extra) > 0 {
			return syncer.Options{}, format, codes.New(codes.InvalidInvocation, "--request does not combine with --%s", extra[0])
		}
		doc, err := loadDocument(*requestPath, stdin)
		if err != nil {
			return syncer.Options{}, format, err
		}
		format, err = response.Select(visited(fs, "format"), *formatFlag, doc.Format)
		if err != nil {
			return syncer.Options{}, format, err
		}
		opt = syncer.Options{
			URL:                 doc.URL,
			Path:                doc.Path,
			Branch:              doc.Branch,
			Username:            doc.Username,
			Password:            doc.Password,
			SSHKey:              []byte(doc.SSHPrivateKey),
			SSHPassphrase:       doc.SSHPassphrase,
			KnownHosts:          doc.SSHKnownHosts,
			InsecureSkipHostKey: doc.InsecureSkipHostKey,
			Replace:             doc.Replace,
		}
		keyPath = doc.SSHPrivateKeyPath
	} else {
		opt = syncer.Options{
			URL:                 *urlFlag,
			Path:                *pathFlag,
			Branch:              *branchFlag,
			Username:            *username,
			Password:            *password,
			SSHPassphrase:       *sshPass,
			KnownHosts:          *knownHosts,
			InsecureSkipHostKey: *insecure,
			Replace:             *replace,
		}
		keyPath = *sshKey
	}
	if *verbose {
		opt.Progress = os.Stderr
	}
	opt.URL = strings.TrimSpace(opt.URL)
	opt.Path = strings.TrimSpace(opt.Path)
	opt.Branch = strings.TrimSpace(opt.Branch)
	opt.Username = strings.TrimSpace(opt.Username)
	opt.KnownHosts = strings.TrimSpace(opt.KnownHosts)
	keyPath = strings.TrimSpace(keyPath)
	if err := applyKey(&opt, keyPath); err != nil {
		return syncer.Options{}, format, err
	}
	if err := syncer.Check(opt); err != nil {
		return syncer.Options{}, format, err
	}
	return opt, format, nil
}

func conflicting(fs *flag.FlagSet) []string {
	var extra []string
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "request", "verbose", "format":
		default:
			extra = append(extra, f.Name)
		}
	})
	return extra
}

func loadDocument(path string, stdin io.Reader) (document, error) {
	var raw []byte
	var err error
	if path == "-" {
		if stdin == nil {
			return document{}, codes.New(codes.MalformedJSON, "stdin is not available")
		}
		raw, err = io.ReadAll(io.LimitReader(stdin, maxRequestBytes+1))
	} else {
		raw, err = readFileLimited(path)
	}
	if err != nil {
		return document{}, codes.New(codes.InvalidInvocation, "cannot read request: %v", err)
	}
	if len(raw) > maxRequestBytes {
		return document{}, codes.New(codes.MalformedJSON, "request exceeds 1MiB")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return document{}, codes.New(codes.MalformedJSON, "request JSON is invalid: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return document{}, codes.New(codes.MalformedJSON, "request must contain a single JSON object")
	}
	return doc, nil
}

func visited(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func readFileLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxRequestBytes+1))
}

func applyKey(opt *syncer.Options, keyPath string) error {
	inline := strings.TrimSpace(string(opt.SSHKey))
	if inline != "" && keyPath != "" {
		return codes.New(codes.BadCredentials, "pass an SSH private key or a key path")
	}
	if keyPath == "" {
		if inline == "" {
			opt.SSHKey = nil
		}
		return nil
	}
	raw, err := readFileLimited(keyPath)
	if err != nil {
		return codes.New(codes.SSHKey, "cannot read SSH private key: %v", err)
	}
	if len(raw) > maxRequestBytes {
		return codes.New(codes.SSHKey, "SSH private key exceeds 1MiB")
	}
	if len(bytesTrimSpace(raw)) == 0 {
		return codes.New(codes.SSHKey, "SSH private key file is empty")
	}
	opt.SSHKey = raw
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// VersionLine is the plain-text version string.
func VersionLine(version string) string {
	return fmt.Sprintf("git-sync %s\n", version)
}
