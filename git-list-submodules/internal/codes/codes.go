// Package codes lists the exit error numbers for git-list-submodules.
//
// 100–199 match the suite-wide registry in GUIDELINES.md.
// 2100–2499 belong only to git-list-submodules.
package codes

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	InvalidInvocation = 100
	MalformedJSON     = 101
	MissingValue      = 102
	Internal          = 199

	InvalidPath   = 2100
	InvalidBranch = 2101

	FolderMissing        = 2200
	NotDirectory         = 2201
	NotRepository        = 2202
	RepositoryUnreadable = 2203
	NoRemoteURL          = 2204

	BranchNotFound = 2300
	NoBranches     = 2301

	ModulesInvalid   = 2400
	SubmoduleMissing = 2401
	SubmoduleURL     = 2402
	SubmodulePath    = 2403
)

// Error is a failure the caller can switch on by Code.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// New builds an Error with a single-line message.
func New(code int, format string, args ...any) *Error {
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	msg = strings.Join(strings.Fields(msg), " ")
	msg = clip(msg, 1024)
	if msg == "" {
		msg = "unknown error"
	}
	return &Error{Code: code, Message: msg}
}

// From maps an arbitrary error onto the public code and message.
func From(err error) (int, string) {
	var ce *Error
	if errors.As(err, &ce) && ce != nil {
		return ce.Code, ce.Message
	}
	if err == nil {
		return Internal, "unknown error"
	}
	return Internal, New(Internal, "%s", err.Error()).Message
}

func clip(msg string, max int) string {
	if len(msg) <= max {
		return msg
	}
	cut := max - 3
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	if cut < 1 {
		return "..."
	}
	return msg[:cut] + "..."
}
