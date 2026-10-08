// Package request reads a git-list-submodules invocation from flags or JSON.
package request

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"git-suite/git-list-submodules/internal/codes"
	"git-suite/git-list-submodules/internal/list"
	"git-suite/git-list-submodules/internal/response"
)

const maxRequestBytes = 1 << 20

// ErrHelp is returned when the caller asked for usage text.
var ErrHelp = errors.New("help")

// ErrVersion is returned when the caller asked for the version.
var ErrVersion = errors.New("version")

// Usage is the standalone help text.
const Usage = `git-list-submodules — list submodules recorded by a branch, or by every local branch.

Usage:
  git-list-submodules --path <dir> [--branch <name>]
  git-list-submodules --request <file|->

The path is resolved against the current working directory.
The process writes one packed JSON document to stdout and exits 0, or exits -1
(reported as 255) with a packed JSON error. --format bson writes one BSON
document instead.

When --branch is omitted, every local branch is listed. The command reads
the repository already in that folder. It does not clone submodules.

Options:
  --path string      repository directory
  --branch string    local branch to inspect (default: all local branches)
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

// Parse turns argv into a validated listing request and the response encoding.
func Parse(args []string, stdin io.Reader) (list.Options, response.Format, error) {
	format := response.JSON
	fs := flag.NewFlagSet("git-list-submodules", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pathFlag := fs.String("path", "", "repository directory")
	branchFlag := fs.String("branch", "", "local branch to inspect")
	requestPath := fs.String("request", "", "JSON request file or -")
	formatFlag := fs.String("format", "", "response encoding: json or bson")
	_ = fs.Bool("verbose", false, "accepted and ignored")
	version := fs.Bool("version", false, "print version")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return list.Options{}, format, ErrHelp
		}
		return list.Options{}, format, codes.New(codes.InvalidInvocation, "%s", err.Error())
	}
	if *version {
		return list.Options{}, format, ErrVersion
	}
	format, err := response.Select(visited(fs, "format"), *formatFlag, "")
	if err != nil {
		return list.Options{}, format, err
	}
	if fs.NArg() != 0 {
		return list.Options{}, format, codes.New(codes.InvalidInvocation, "unexpected argument %q", fs.Arg(0))
	}

	var opt list.Options
	if *requestPath != "" {
		if extra := conflicting(fs); len(extra) > 0 {
			return list.Options{}, format, codes.New(codes.InvalidInvocation, "--request does not combine with --%s", extra[0])
		}
		doc, err := loadDocument(*requestPath, stdin)
		if err != nil {
			return list.Options{}, format, err
		}
		format, err = response.Select(visited(fs, "format"), *formatFlag, doc.Format)
		if err != nil {
			return list.Options{}, format, err
		}
		opt = list.Options{Path: doc.Path, Branch: doc.Branch}
	} else {
		opt = list.Options{Path: *pathFlag, Branch: *branchFlag}
	}
	opt.Path = strings.TrimSpace(opt.Path)
	opt.Branch = strings.TrimSpace(opt.Branch)
	if err := list.Check(opt); err != nil {
		return list.Options{}, format, err
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
	return fmt.Sprintf("git-list-submodules %s\n", version)
}
