# git-sync

Clone a repository into a directory, or refresh that directory when it already contains the same repository. The binary talks to the remote itself. The machine that runs it does not need git installed.

## Build

Requires Go 1.26 or newer.

```bash
cd git-sync
CGO_ENABLED=0 go build -o git-sync .
```

`CGO_ENABLED=0` produces a static binary. The program uses a pure-Go git implementation, so the target machine does not need git or libgit2.

## Usage

Paths are resolved against the current working directory.

```bash
./git-sync --url "https://github.com/org/repo.git" --path "src/repo"
```

HTTP credentials:

```bash
./git-sync \
  --url "https://github.com/org/repo.git" \
  --path "src/repo" \
  --username "git" \
  --password "$TOKEN"
```

SSH, with a pinned host key:

```bash
./git-sync \
  --url "ssh://git@github.com/org/repo.git" \
  --path "src/repo" \
  --ssh-key "$HOME/.ssh/id_ed25519" \
  --ssh-known-hosts "$HOME/.ssh/known_hosts"
```

`--insecure-skip-host-key` accepts any SSH host key. Prefer a `known_hosts` file.

JSON on stdin is the form other programs should use. Flags are visible in the process list.

```bash
./git-sync --request - <<'EOF'
{
  "url": "ssh://git@github.com/org/repo.git",
  "path": "src/repo",
  "branch": "main",
  "ssh_private_key_path": "/keys/id_ed25519",
  "ssh_known_hosts": "/keys/known_hosts"
}
EOF
```

`--request` also accepts a file path. `--verbose` and `--format` may appear with it. `--verbose` writes transfer progress to stderr. `--format` is `json` (the default) or `bson`.

### Request fields

| Field | Required | Meaning |
| --- | --- | --- |
| `url` | yes | `http`, `https`, `ssh://`, `git@host:path`, or an absolute `file://` URL |
| `path` | yes | Destination directory, relative to the working directory or absolute |
| `branch` | no | Branch to check out. Omitted means the remote default branch |
| `username`, `password` | for private HTTP | Both are required together. The password may be a token |
| `ssh_private_key` | for SSH | PEM private key text |
| `ssh_private_key_path` | for SSH | Private key file. Use this or `ssh_private_key` |
| `ssh_passphrase` | no | Passphrase for an encrypted private key |
| `ssh_known_hosts` | for SSH | `known_hosts` file used to verify the server |
| `insecure_skip_host_key` | no | Accept any SSH host key |
| `replace` | no | Overwrite a non-empty directory that is not a git repository |
| `format` | no | Response encoding: `json` (default) or `bson` |

A password inside the URL is rejected. Pass it in `password` so it is not stored in `.git/config`.

## What it does

1. The destination is missing, or it is an empty directory: clone into it.
2. The destination is a git checkout of the same repository (the same host and path; `https` and `ssh` match, and a trailing `.git` does not matter): fetch, then make the worktree match the remote branch. Local commits, edits, and untracked files are removed.
3. The destination is a different git repository, or its `.git` directory cannot be read: clone to a temporary directory and swap it into place after the clone succeeds.
4. The destination is a non-empty directory that is not a git repository: stop with code `1201` and leave it untouched. `replace` overwrites it, using the same clone-then-swap sequence.
5. The destination exists and is not a directory: stop with code `1200`.

A failed clone does not remove the previous directory. An authentication failure or an unknown branch does not replace a good checkout. If the process is killed during a clone, a hidden `.git-sync-*` directory can remain beside the destination; it is safe to delete.

The process writes one packed document. JSON is a single line with a trailing newline. BSON, selected with `--format bson` or `"format":"bson"`, is the raw document and has no trailing newline.

Success:

```json
{"status":"ok","path":"/abs/src/repo","url":"https://github.com/org/repo.git","action":"cloned","branch":"main","commit":"01234567"}
```

`action` is `cloned`, `updated`, or `unchanged`. `path` is absolute. `commit` is the first 8 characters of the hash now checked out.

Failure uses the same encoding:

```json
{"status":"failed","code":1201,"message":"directory is not a git repository and is not empty; pass replace to overwrite it"}
```

The process exits `0` on success and `-1` on failure. Unix reports that failure status as `255`.

`--help` and `--version` write plain text and exit `0`.

## Calling it

Read stdout only. With `--verbose`, stderr is progress text.

```bash
if output=$(./git-sync --request payload.json); then
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
| 1100 | Repository URL is invalid |
| 1101 | Destination path is invalid |
| 1102 | Branch name is invalid |
| 1103 | Credentials are missing or contradictory |
| 1200 | Path exists and is not a directory |
| 1201 | Directory is not a git repository and is not empty |
| 1202 | Destination directory cannot be prepared |
| 1203 | Clone succeeded but could not be moved into place |
| 1301 | SSH private key cannot be read or parsed |
| 1302 | SSH host key verification was not configured |
| 1303 | Authentication or authorization failed |
| 1400 | Clone failed |
| 1401 | Local repository cannot be read |
| 1402 | Update failed, and the replacement clone also failed |
| 1403 | Requested branch was not found |

## Limitations

Submodules are not cloned or updated. Git LFS files stay as the pointer text stored in the repository. The tool does not use `ssh-agent` or `~/.ssh/config`. A non-default SSH port uses an `ssh://git@host:2222/org/repo.git` URL.
