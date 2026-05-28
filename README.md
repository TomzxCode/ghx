# ghx

Extended GitHub CLI that provides pull request and issue comment operations not available in the standard `gh` CLI.
Uses the GitHub GraphQL API directly.

## Requirements

- A GitHub token set via `GH_TOKEN` or `GITHUB_TOKEN`, or the [gh](https://cli.github.com/) CLI authenticated with `gh auth login` (used as fallback)

## Install

Download the latest binary from [releases](https://github.com/tomzxcode/ghx/releases).

Or build from source (requires [Go](https://go.dev/) 1.21+):

```bash
go install github.com/tomzxcode/ghx@main
```

## Usage

All commands accept `--repo OWNER/REPO` (or `-R`) to target a specific repository. If omitted, the repository is detected from the current git remote.

### Comment on a PR

Top-level comment:

```bash
$ ghx pr comment 42 --body "Looks good"
Created comment PR_kwDOABC123 on PR #42
```

Inline comment on a file:

```bash
$ ghx pr comment 42 --file src/main.go --line 10 --body "Nit: use fmt.Errorf"
Created inline comment on src/main.go:10 (thread PRRT_kwDOABC456)
```

Inline comment on a line range:

```bash
$ ghx pr comment 42 --file src/main.go --line 10-15 --body "Consider extracting this"
Created inline comment on src/main.go:10-15 (thread PRRT_kwDOABC456)
```

File-level comment:

```bash
$ ghx pr comment 42 --file src/main.go --body "Overall looks clean"
Created file-level comment on src/main.go (thread PRRT_kwDOABC789)
```

Reply to an existing thread:

```bash
$ ghx pr comment 42 --reply-thread <thread-id> --body "Agreed"
Replied to thread PRRT_kwDOABC456 (comment PRC_kgDOABC000)
```

Read body from stdin or a file:

```bash
ghx pr comment 42 --body-file -
ghx pr comment 42 --body-file comment.txt
```

### Add comments to a pending review

Use `--pending` to add comments to a pending review instead of submitting them immediately:

```bash
$ ghx pr comment 42 --file src/main.go --line 10 --body "Nit" --pending
Added pending inline comment on src/main.go:10 (thread PRRT_kwDOABC456, review PRR_kwDOABC123)

$ ghx pr comment 42 --reply-thread <thread-id> --body "Reply" --pending
Added pending reply to thread PRRT_kwDOABC456 (comment PRC_kgDOABC000, review PRR_kwDOABC123)
```

Submit the review when ready:

```bash
$ ghx pr review submit 42 --event COMMENT
Submitted review PRR_kwDOABC123 as COMMENT

$ ghx pr review submit 42 --event APPROVE --body "LGTM"
Submitted review PRR_kwDOABC123 as APPROVE
```

### Add comments to a local stash

Use `--stash` to save comments directly to a local stash entry without any GitHub API calls.
Each stash entry is stored as a separate YAML file on disk.
Supports multiple stash entries like `git stash`:

```bash
$ ghx pr comment 42 --file src/main.go --line 10 --body "Nit" --stash
Stashed comment on src/main.go:10 (stash@{0} now has 1 threads)

$ ghx pr comment 42 --file src/main.go --line 20-25 --body "Consider extracting" --stash
Stashed comment on src/main.go:20-25 (stash@{0} now has 2 threads)

$ ghx pr review stash list 42
stash@{0}:  nit comments  (2 threads, 2 comments)
	src/main.go  10     1 comment(s)
	src/main.go  20-25  1 comment(s)

$ ghx pr review stash pop 42
Popped stash@{0} (2 threads, 2 comments) into review PRR_kwDOABC123
```

Add to a specific stash entry:

```bash
ghx pr comment 42 --file src/main.go --line 30 --body "Another" --stash=1
```

### Manage pending reviews

```bash
$ ghx pr review create 42
Created pending review PRR_kwDOABC123 on PR #42

$ ghx pr review list 42
REVIEW_ID             AUTHOR      CREATED
PRR_kwDOABC123        octocat     2025-01-15T10:30:00Z

$ ghx pr review discard <review-id>
Discarded review PRR_kwDOABC123
```

### Stash pending review comments

Save pending review comments to local disk so you can post immediate comments, or accumulate review comments offline:

```bash
$ ghx pr review stash push 42
Saved stash@{0} (3 threads, 5 comments) from review PRR_kwDOABC123

$ ghx pr review stash push 42 -m "nit comments"
Saved stash@{0} "nit comments" (3 threads, 5 comments) from review PRR_kwDOABC123

$ ghx pr review stash push 42
Saved stash@{0} (2 threads, 3 comments) from review PRR_kwDOABC456
(2 stash entries total)

$ ghx pr review stash list 42
stash@{0}:  2 threads, 3 comments
	src/main.go  10     2 comment(s)
	src/util.go  5-8    1 comment(s)

stash@{1}:  nit comments  (3 threads, 5 comments)
	src/main.go  10     2 comment(s)
	src/main.go  20-25  1 comment(s)
	src/util.go  30     2 comment(s)

$ ghx pr review stash pop 42
Popped stash@{0} (2 threads, 3 comments) into review PRR_kwDOABC789

$ ghx pr review stash pop 42 --stash 1
Popped stash@{1} (3 threads, 5 comments) into review PRR_kwDOABC789

$ ghx pr review stash drop 42
Dropped stash@{0} (2 threads, 3 comments)
```

### List review threads

```bash
$ ghx pr threads 42
src/main.go:10  [open]
octocat  This logic could be simplified
reviewer  Agreed, let's refactor

src/util.go:5-8  [resolved]
reviewer  Consider using a helper function
octocat  Good idea, done in f8a3b1c

$ ghx pr threads 42 --ids
PRRT_kwDOABC456  src/main.go:10  [open]
PRC_kgDOABC001   octocat  This logic could be simplified
PRC_kgDOABC002   reviewer  Agreed, let's refactor
```

Show a specific thread or filter by state:

```bash
ghx pr threads 42 --thread <thread-id>
ghx pr threads 42 --state all
ghx pr threads 42 --state resolved
```

### Edit a comment

```bash
$ ghx pr comment edit <comment-id> --body "Updated text"
Updated comment PRC_kgDOABC001
```

```bash
ghx pr comment edit <comment-id> --body-file updated.txt
```

When submitting an immediate inline comment or reply (without `--pending`) on a PR that has an existing pending review, ghx will temporarily stash the pending review comments to disk, submit the comment, and then restore them.
This is necessary because GitHub does not allow mixing immediate and pending review comments on the same PR.

Automatically detects whether the PR comment is a review comment or an issue comment.

### Delete a comment

```bash
$ ghx pr comment delete <comment-id>
Deleted comment PRC_kgDOABC001
```

Use `ghx pr threads <number> --ids` to find comment IDs.

### Comment on an issue

```bash
$ ghx issue comment 42 --body "This is fixed in #50"
Created comment IC_kwDOABC123 on issue #42
```

Read body from stdin or a file:

```bash
ghx issue comment 42 --body-file -
ghx issue comment 42 --body-file comment.txt
```

### View an issue

```bash
$ ghx issue view 42
Fix null pointer in parser  [open]  octocat

The parser crashes when encountering empty input.

2 comment(s):

octocat  I can reproduce this on v1.2.0
reviewer  Fixed in #50, closing

$ ghx issue view 42 --ids
Fix null pointer in parser  [open]  octocat

The parser crashes when encountering empty input.

2 comment(s):

IC_kwDOABC001  octocat  I can reproduce this on v1.2.0
IC_kwDOABC002  reviewer  Fixed in #50, closing
```

### Edit an issue comment

```bash
$ ghx issue comment edit <comment-id> --body "Updated text"
Updated comment IC_kwDOABC001
```

```bash
ghx issue comment edit <comment-id> --body-file updated.txt
```

### Delete an issue comment

```bash
$ ghx issue comment delete <comment-id>
Deleted comment IC_kwDOABC001
```

Use `ghx issue view <number> --ids` to find comment IDs.

## Alternatives

- [gh-pr-review](https://github.com/agynio/gh-pr-review) - AI-powered PR review assistant using GitHub Actions

## License

[MIT](LICENSE)
