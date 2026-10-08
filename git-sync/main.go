// Command git-sync clones a git repository into a directory, or refreshes
// that directory when it already contains the same repository.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"git-suite/git-sync/internal/codes"
	"git-suite/git-sync/internal/request"
	"git-suite/git-sync/internal/response"
	"git-suite/git-sync/internal/syncer"
)

const version = "0.1.0"

func main() {
	code := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if code == 0 {
		os.Exit(0)
	}
	os.Exit(-1)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	format := response.JSON
	defer func() {
		if rec := recover(); rec != nil {
			response.Write(stdout, format, response.Failure{
				Status:  "failed",
				Code:    codes.Internal,
				Message: "unexpected internal error",
			})
			fmt.Fprintf(stderr, "panic: %v\n%s\n", rec, debug.Stack())
			code = -1
		}
	}()

	opt, parsed, err := request.Parse(args, stdin)
	format = parsed
	switch {
	case err == nil:
	case errors.Is(err, request.ErrHelp):
		fmt.Fprint(stdout, request.Usage)
		return 0
	case errors.Is(err, request.ErrVersion):
		fmt.Fprint(stdout, request.VersionLine(version))
		return 0
	default:
		response.WriteError(stdout, format, err)
		return -1
	}

	result, err := syncer.Sync(context.Background(), opt)
	if err != nil {
		response.WriteError(stdout, format, err)
		return -1
	}
	response.Write(stdout, format, result)
	return 0
}
