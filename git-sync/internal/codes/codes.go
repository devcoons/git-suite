// Package codes lists the exit error numbers for git-sync.
//
// 100–199 match the suite-wide registry in GUIDELINES.md.
// 1100–1499 belong only to git-sync.
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

	InvalidURL     = 1100
	InvalidPath    = 1101
	InvalidBranch  = 1102
	BadCredentials = 1103

	NotDirectory    = 1200
	RefusingReplace = 1201
	Destination     = 1202
	ReplaceFailed   = 1203

	SSHKey        = 1301
	HostKeyConfig = 1302
	AuthFailed    = 1303

	CloneFailed          = 1400
	RepositoryUnreadable = 1401
	UpdateFailed         = 1402
	BranchNotFound       = 1403
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

// clip keeps a message on one UTF-8 boundary so a long line cannot split a
// character or grow without a limit.
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
