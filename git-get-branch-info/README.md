# git-get-branch-info

Describe one local branch, or every local branch, in a repository directory. The binary reads commits stored in that repository. The machine that runs it does not need git installed.

For each branch the command walks every commit reachable from the tip.

## Build

Requires Go 1.26 or newer.

```bash
cd git-get-branch-info
CGO_ENABLED=0 go build -o git-get-branch-info .
```

## Usage

The path is resolved against the current working directory.

```bash
./git-get-branch-info --path "src/repo"
./git-get-branch-info --path "src/repo" --branch "main"
```

JSON on stdin:

```bash
./git-get-branch-info --request - <<'EOF'
{"path":"src/repo","branch":"main"}
EOF
```

`--request` also accepts a file path. `--verbose` and `--format` may appear with it. This command has no progress output, so `--verbose` is accepted and ignored. `--format` is `json` (default) or `bson`.

Omit `branch` to describe every local branch. Remote-tracking refs are not included. Branch names are sorted.

### Request fields

| Field | Required | Meaning |
| --- | --- | --- |
| `path` | yes | Repository directory, relative to the working directory or absolute |
| `branch` | no | Local branch to describe. Omitted means every local branch |
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
      "checked_out": true,
      "commit_count": 2,
      "root_count": 1,
      "first_commit_id": "01234567",
      "first_commit_at": "2020-01-01T00:00:00Z",
      "first_committer_name": "Ada Lovelace",
      "first_committer_email": "ada@example.com",
      "last_commit_id": "89abcdef",
      "last_commit_at": "2020-02-01T00:00:00Z",
      "last_committer_name": "Ada Lovelace",
      "last_committer_email": "ada@example.com",
      "last_commit_subject": "second change"
    }
  ]
}
```

`path` is the absolute repository directory. `url` is the `origin` remote URL stored in the repository. Commit ids are the first 8 characters of the hash. Times are the committer time in UTC RFC 3339, which is when the commit was created.

`commit_count` is the number of commits reachable from the branch tip, including the tip. `last_commit_id` is that tip. `last_commit_subject` is the first line of its message, at most 1024 bytes. Committer names and emails are limited the same way.

`first_commit_id` is the earliest root commit in that history. A root commit has no parents. `root_count` is 1 for a normal history. It is greater than 1 when unrelated histories were merged; the first commit is then the root with the earliest committer time.

`checked_out` is true for the branch currently checked out in the working tree. A detached HEAD leaves every branch false.

The committer name and email are included with the first and last commits so a caller can see who created them without a second lookup.

## Left for a later command

Ahead and behind an upstream branch are not included. Those counts depend on a remote-tracking ref that may not have been fetched, and this command does not contact a remote. The changed-file counts of the last commit are also omitted; they describe a diff, not the branch itself.

## Failure

```json
{"status":"failed","code":4200,"message":"folder does not exist"}
```

The process exits `0` on success and `-1` on failure. Unix reports that failure status as `255`.

`--help` and `--version` write plain text and exit `0`.

## Calling it

Read stdout only.

```bash
if output=$(./git-get-branch-info --path "src/repo"); then
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
| 4100 | Path is invalid |
| 4101 | Branch name is invalid |
| 4200 | Folder does not exist |
| 4201 | Path exists and is not a directory |
| 4202 | Folder is not a git repository |
| 4203 | Repository cannot be read |
| 4204 | Repository has no origin URL |
| 4300 | Branch was not found |
| 4301 | Repository has no branches |
| 4400 | The branch history cannot be read |
