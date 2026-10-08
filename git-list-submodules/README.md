# git-list-submodules

List the submodules recorded by one local branch, or by every local branch, in a repository directory. The binary reads that repository itself. It does not clone submodules, and the machine that runs it does not need git installed.

A submodule is reported only when that branch's commit contains both pieces of the record:

- a `.gitmodules` entry with a URL and a path
- a gitlink at that path, which holds the submodule commit id

If either piece is missing, the command fails. An empty list means the branch records no submodules. The submodule directory does not have to be checked out in the working tree.

## Build

Requires Go 1.26 or newer.

```bash
cd git-list-submodules
CGO_ENABLED=0 go build -o git-list-submodules .
```

## Usage

The path is resolved against the current working directory.

```bash
./git-list-submodules --path "src/repo"
./git-list-submodules --path "src/repo" --branch "main"
```

JSON on stdin:

```bash
./git-list-submodules --request - <<'EOF'
{"path":"src/repo","branch":"main"}
EOF
```

`--request` also accepts a file path. `--verbose` and `--format` may appear with it. This command has no progress output, so `--verbose` is accepted and ignored. `--format` is `json` (the default) or `bson`.

Omit `branch` to list every local branch. Remote-tracking refs are not included. Branch names are sorted, and submodules within a branch are sorted by path.

### Request fields

| Field | Required | Meaning |
| --- | --- | --- |
| `path` | yes | Repository directory, relative to the working directory or absolute |
| `branch` | no | Local branch to inspect. Omitted means every local branch |
| `format` | no | Response encoding: `json` (default) or `bson` |

## Success

The process writes one packed document. JSON is a single line with a trailing newline. BSON, selected with `--format bson` or `"format":"bson"`, is the raw document and has no trailing newline. The example below is expanded so the fields are easy to read.

```json
{
  "status": "ok",
  "path": "/abs/src/repo",
  "url": "https://example.com/repo.git",
  "branches": [
    {
      "branch": "main",
      "submodules": [
        {
          "url": "https://example.com/lib.git",
          "path": "libs/lib",
          "commit_id": "01234567"
        }
      ]
    }
  ]
}
```

`path` is the absolute repository directory. `url` is the `origin` remote URL stored in the repository. Each submodule `path` is the path inside that repository. `commit_id` is the first 8 characters of the hash the branch records for that submodule. Each submodule `url` is the URL written in `.gitmodules` for that commit, including a relative URL when that is what the file contains.

A branch with no submodules is included with `"submodules": []`.

## Failure

```json
{"status":"failed","code":2200,"message":"folder does not exist"}
```

The process exits `0` on success and `-1` on failure. Unix reports that failure status as `255`.

`--help` and `--version` write plain text and exit `0`.

## Calling it

Read stdout only.

```bash
if output=$(./git-list-submodules --path "src/repo"); then
  printf '%s\n' "$output"
else
  status=$?
  printf '%s\n' "$output" >&2
  exit "$status"
fi
```

In Go, `exec.Command(...).Output()` returns the JSON document. On failure, `ExitCode()` is `255` and the document is still in the returned stdout.

## Error codes

| Code | Meaning |
| --- | --- |
| 100 | Invalid invocation |
| 101 | Request JSON is malformed or larger than 1 MiB |
| 102 | A required value is missing |
| 199 | Internal fault |
| 2100 | Path is invalid |
| 2101 | Branch name is invalid |
| 2200 | Folder does not exist |
| 2201 | Path exists and is not a directory |
| 2202 | Folder is not a git repository |
| 2203 | Repository cannot be read |
| 2204 | Repository has no origin URL |
| 2300 | Branch was not found |
| 2301 | Repository has no branches |
| 2400 | `.gitmodules` is invalid |
| 2401 | A declared submodule path does not exist on that branch, or the path is not a gitlink |
| 2402 | A submodule gitlink has no URL in `.gitmodules` |
| 2403 | A submodule path is invalid |

## Limitations

The command lists the direct submodules of the repository in the given folder. It does not walk submodules of those submodules, and it does not report tags. A tag lives in the submodule repository, which this command does not clone.
