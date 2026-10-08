# git-suite

Small, independent programs for git operations on a machine that does not have git installed. Each program is a static executable, stateless, and returns one packed JSON document, or BSON when asked.

The shared contract is [GUIDELINES.md](GUIDELINES.md).

| App | Purpose |
| --- | --- |
| [git-sync](git-sync/README.md) | Clone a repository into a directory, or refresh it when that directory already holds the same repository. |
| [git-list-submodules](git-list-submodules/README.md) | List the submodules recorded by one branch, or by every local branch. |
| [git-list-tags](git-list-tags/README.md) | List the tags contained in one branch, or in every local branch. |
| [git-get-branch-info](git-get-branch-info/README.md) | Describe one local branch, or every local branch: commit count, first commit, and last commit. |
