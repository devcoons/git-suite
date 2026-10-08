# git-list-tags

List the tags contained in one local branch, or in every local branch, of a repository directory. The binary reads that repository itself. The machine that runs it does not need git installed.

A tag belongs to a branch when the tagged commit is reachable from that branch's tip. The same tag can appear under more than one branch. Every tag ref in the repository must point at an object stored in that repository and peel to a commit. A tag whose object is missing, or that points at a tree or a blob, fails the command.

## Build

Requires Go 1.26 or newer.

```bash
cd git-list-tags
CGO_ENABLED=0 go build -o git-list-tags .
```

## Usage

The path is resolved against the current working directory.

```bash
./git-list-tags --path "src/repo"
./git-list-tags --path "src/repo" --branch "main"
```

JSON on stdin:

```bash
./git-list-tags --request - <<'EOF'
{"path":"src/repo","branch":"main"}
EOF
```

`--request` also accepts a file path. `--verbose` and `--format` may appear with it. This command has no progress output, so `--verbose` is accepted and ignored. `--format` is `json` (the default) or `bson`.

Omit `branch` to list every local branch. Remote-tracking refs are not included. Branch names are sorted, and tags within a branch are sorted by name.

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
      "tags": [
        {
          "name": "v1.0.0",
          "commit_id": "01234567",
          "when": "2026-10-08T18:00:00Z",
          "tagger_name": "Ada Lovelace",
          "tagger_email": "ada@example.com",
          "annotated": true,
          "signed": false
        }
      ]
    }
  ]
}
```

`path` is the absolute repository directory. `url` is the `origin` remote URL stored in the repository. `commit_id` is the first 8 characters of the hash of the tagged commit. `when` is UTC RFC 3339.

An annotated tag uses its own tagger for `tagger_name`, `tagger_email`, and `when`. A lightweight tag does not record a person or a time, so those three fields come from the tagged commit's committer, and `annotated` is false. Names and emails are limited to 1024 bytes.

`signed` is true when the tag object contains a signature. The signature is not checked against a key. A lightweight tag is never signed.

A branch with no tags is included with `"tags": []`. A repository with no tags succeeds.

## Failure

```json
{"status":"failed","code":3200,"message":"folder does not exist"}
```

The process exits `0` on success and `-1` on failure. Unix reports that failure status as `255`.

`--help` and `--version` write plain text and exit `0`.

## Calling it

Read stdout only.

```bash
if output=$(./git-list-tags --path "src/repo"); then
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
| 3100 | Path is invalid |
| 3101 | Branch name is invalid |
| 3200 | Folder does not exist |
| 3201 | Path exists and is not a directory |
| 3202 | Folder is not a git repository |
| 3203 | Repository cannot be read |
| 3204 | Repository has no origin URL |
| 3300 | Branch was not found |
| 3301 | Repository has no branches |
| 3400 | A tag's target object does not exist in the repository, or the tag cannot be read |
| 3401 | A tag does not point at a commit |

## Limitations

The command reads tags already stored in the given repository. It does not fetch tags from a remote. `signed` means a signature is present, not that the signature is valid.
