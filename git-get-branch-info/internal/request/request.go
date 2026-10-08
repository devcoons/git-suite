// Package request reads a git-get-branch-info invocation from flags or JSON.
package request

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"git-suite/git-get-branch-info/internal/codes"
	"git-suite/git-get-branch-info/internal/info"
	"git-suite/git-get-branch-info/internal/response"
)

const maxRequestBytes = 1 << 20

// ErrHelp is returned when the caller asked for usage text.
var ErrHelp = errors.New("help")

// ErrVersion is returned when the caller asked for the version.
var ErrVersion = errors.New("version")

// Usage is the standalone help text.
const Usage = `git-get-branch-info — describe a local branch, or every local branch.

Usage:
  git-get-branch-info --path <dir> [--branch <name>]
  git-get-branch-info --request <file|->

The path is resolved against the current working directory.
The process writes one packed JSON document to stdout and exits 0, or exits -1
(reported as 255) with a packed JSON error. --format bson writes one BSON
document instead.

When --branch is omitted, every local branch is described. The command reads
commits already stored in the repository.

Options:
  --path string      repository directory
  --branch string    local branch to describe (default: all local branches)
  --request string   JSON request file, or - for stdin
  --format string    response encoding: json (default) or bson
  --verbose          accepted, and ignored; this command has no progress output
  --version          print the version and exit
`

type document struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Format string `json:"format"`
}

// Parse turns argv into a validated request and the response encoding.
func Parse(args []string, stdin io.Reader) (info.Options, response.Format, error) {
	format := response.JSON
	fs := flag.NewFlagSet("git-get-branch-info", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pathFlag := fs.String("path", "", "repository directory")
	branchFlag := fs.String("branch", "", "local branch to describe")
	requestPath := fs.String("request", "", "JSON request file or -")
	formatFlag := fs.String("format", "", "response encoding: json or bson")
	_ = fs.Bool("verbose", false, "accepted and ignored")
	version := fs.Bool("version", false, "print version")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return info.Options{}, format, ErrHelp
		}
		return info.Options{}, format, codes.New(codes.InvalidInvocation, "%s", err.Error())
	}
	if *version {
		return info.Options{}, format, ErrVersion
	}
	format, err := response.Select(visited(fs, "format"), *formatFlag, "")
	if err != nil {
		return info.Options{}, format, err
	}
	if fs.NArg() != 0 {
		return info.Options{}, format, codes.New(codes.InvalidInvocation, "unexpected argument %q", fs.Arg(0))
	}

	var opt info.Options
	if *requestPath != "" {
		if extra := conflicting(fs); len(extra) > 0 {
			return info.Options{}, format, codes.New(codes.InvalidInvocation, "--request does not combine with --%s", extra[0])
		}
		doc, err := loadDocument(*requestPath, stdin)
		if err != nil {
			return info.Options{}, format, err
		}
		format, err = response.Select(visited(fs, "format"), *formatFlag, doc.Format)
		if err != nil {
			return info.Options{}, format, err
		}
		opt = info.Options{Path: doc.Path, Branch: doc.Branch}
	} else {
		opt = info.Options{Path: *pathFlag, Branch: *branchFlag}
	}
	opt.Path = strings.TrimSpace(opt.Path)
	opt.Branch = strings.TrimSpace(opt.Branch)
	if err := info.Check(opt); err != nil {
		return info.Options{}, format, err
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

func visited(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
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

func readFileLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxRequestBytes+1))
}

// VersionLine is the plain-text version string.
func VersionLine(version string) string {
	return fmt.Sprintf("git-get-branch-info %s\n", version)
}
