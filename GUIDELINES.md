# git-suite requirements and guidelines

These rules apply to every mini application in this suite. A new app follows them as written so callers can treat the programs as one family.

## Purpose

The target host does not have git installed. Each program is a self-contained executable that performs one git operation and then exits. Programs are independent of each other and hold no state between calls.

## Language

Write apps in Go.

Use Rust only when a measured property of that app requires it: sustained CPU work, a memory ceiling Go cannot meet, or a library that has no workable Go equivalent. Clone, fetch, and reading commits are dominated by network and disk, and `go-git` implements them in pure Go. That is the implementation behind `git-sync`, `git-list-submodules`, `git-list-tags`, and `git-get-branch-info`.

Build with `CGO_ENABLED=0` so the binary does not depend on libc or libgit2.

## Layout

```
git-suite/
  GUIDELINES.md
  README.md
  <app>/
    README.md
    go.mod
    main.go
    internal/...
```

The directory name is the binary name. Each app is its own Go module. Shared behavior is specified here and implemented inside the app, so one app can be built and shipped alone.

## Statelessness

Every input arrives on the command line or on stdin for that invocation. The app keeps no config file, cache, or credential store. It does not read `ssh-agent`, `GIT_ASKPASS`, or ambient git identity. An HTTP client may still honor the process proxy variables `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`, because those are supplied by the caller for that process.

## Process contract

| Outcome | stdout | stderr | exit status |
| --- | --- | --- | --- |
| Success | One packed document, JSON by default or BSON when requested | Empty, unless a documented verbose flag is set | `0` |
| Failure | One packed document in the same encoding | Empty, unless a panic stack is being reported | `-1` |
| `--help`, `--version` | Plain text | Empty | `0` |

On Unix the status `-1` is reported to the parent as `255`. `os/exec` `ExitCode()` therefore returns `255`. The document body is the error contract; the status only signals failure.

A panic is recovered. The caller still receives a failure document with code `199`, and the stack is written to stderr.

stdout carries exactly one document. JSON is packed, with no extra space or line breaks, and ends with a newline. BSON is the raw document and has no trailing newline. Progress, if requested, goes to stderr so a pipe of stdout stays parseable.

## Response

The encoding is `json` unless the caller asks for `bson`. Pass `--format bson`, or set `"format":"bson"` in the request document. `--format` may appear beside `--request`. When both are set they must name the same encoding. An omitted or empty format means JSON. Any other name is code `100`.

If the invocation fails before a format can be read, the failure document is JSON.

Failure, every app:

```json
{"status":"failed","code":1100,"message":"repository URL is invalid"}
```

- `status` is the string `failed`.
- `code` is a number from the registry below.
- `message` is one line of plain text for a human. Callers branch on `code`.

Success, every app, begins with:

```json
{"status":"ok","path":"/abs/src/repo","url":"https://example.com/repo.git"}
```

- `status` is the string `ok`.
- `path` is the absolute repository directory.
- `url` is the repository URL. `git-sync` returns the URL it cloned or refreshed. The other apps return the `origin` remote URL stored in that repository, and fail when `origin` has no URL.

Further fields belong to the app and are documented in that app's README. Field names are `snake_case` in requests and responses.

## Error numbers

| Range | Owner |
| --- | --- |
| 100–199 | Every app, same meaning |
| 1000–1999 | `git-sync` |
| 2000–2999 | `git-list-submodules` |
| 3000–3999 | `git-list-tags` |
| 4000–4999 | `git-get-branch-info` |
| 5000–5999 | Next app |
| +1000 | Each later app |

Common codes:

| Code | Meaning |
| --- | --- |
| 100 | Invalid invocation (unknown flag, conflicting options) |
| 101 | Request JSON is malformed or too large |
| 102 | A required value is missing |
| 199 | Internal fault recovered from a panic, or an unclassified failure |

An app lists its own codes in its README and in a single `codes` package. Numbers are stable once published.

## Command line

- Flags use lowercase words separated by hyphens.
- A repeatable machine invocation is `--request <file>` or `--request -` for a JSON object on stdin.
- `--request` accepts `--verbose` and `--format` beside it. The JSON object is the whole operation. `format` in that object, or `--format`, chooses the response encoding.
- Secrets are fields of that JSON object. Flags exist for interactive use; a process list can see them.
- Relative paths are resolved against the process working directory, which is the caller's directory.
- The request body is limited to 1 MiB.
- Unknown JSON fields are rejected.

## Secrets

Credential material is passed per call and is not written into `.git/config` or into the success document. Passwords and key passphrases are stripped from error text when they are long enough to match safely. URLs that embed a password are rejected; the caller passes `username` and `password`, or an SSH key, as separate fields.

## Failure behavior

The program returns the failure document and exits. It does not retry forever, prompt, or open a shell. If a later attempt can still succeed, the app's README describes that attempt and what is left on disk when both attempts fail.

## Build and test

From the app directory:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o <app> .
```

Tests cover the success path, a wrong input, and the packed JSON failure shape. Tests may use the system `git` to build fixtures. The shipped binary does not.

## Adding an app

1. Pick the next free thousand-block of error codes and record it in this file.
2. Create `<app>/` as its own module.
3. Implement the process contract, including panic recovery and exit status `-1`.
4. Write `<app>/README.md` with the request, the success document, the failure codes, and a copy-paste invocation.
5. Add the app to the root `README.md`.
