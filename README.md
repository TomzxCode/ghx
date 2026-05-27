# ghx

Extended GitHub CLI that provides pull request review operations not available in the standard `gh` CLI.
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

```bash
# Top-level comment
ghx pr comment 42 --body "Looks good"

# Inline comment on a file
ghx pr comment 42 --file src/main.go --line 10 --body "Nit: use fmt.Errorf"

# Inline comment on a line range
ghx pr comment 42 --file src/main.go --line 10-15 --body "Consider extracting this"

# File-level comment
ghx pr comment 42 --file src/main.go --body "Overall looks clean"

# Reply to an existing thread
ghx pr comment 42 --reply-thread <thread-id> --body "Agreed"

# Read body from stdin or a file
ghx pr comment 42 --body-file -
ghx pr comment 42 --body-file comment.txt
```

### Add comments to a pending review

Use `--pending` to add comments to a pending review instead of submitting them immediately:

```bash
ghx pr comment 42 --file src/main.go --line 10 --body "Nit" --pending
ghx pr comment 42 --reply-thread <thread-id> --body "Reply" --pending
```

Submit the review when ready:

```bash
ghx pr review submit 42 --event COMMENT
ghx pr review submit 42 --event APPROVE --body "LGTM"
```

### Manage pending reviews

```bash
# Create a pending review
ghx pr review create 42

# List your pending reviews
ghx pr review list 42

# Discard a pending review
ghx pr review discard <review-id>
```

### List review threads

```bash
# List open threads
ghx pr threads 42

# Show a specific thread
ghx pr threads 42 --thread <thread-id>

# Include comment IDs (for edit/delete)
ghx pr threads 42 --ids

# Filter by state
ghx pr threads 42 --state all
ghx pr threads 42 --state resolved
```

### Edit a comment

```bash
ghx pr comment edit <comment-id> --body "Updated text"
ghx pr comment edit <comment-id> --body-file updated.txt
```

When submitting an immediate inline comment or reply (without `--pending`) on a PR that has an existing pending review, ghx will temporarily discard the pending review, submit the comment, and then restore the pending review with its comments.
This is necessary because GitHub does not allow mixing immediate and pending review comments on the same PR.

Automatically detects whether the comment is a review comment or an issue comment.

### Delete a comment

```bash
ghx pr comment delete <comment-id>
```

Use `ghx pr threads <number> --ids` to find comment IDs.

## License

[MIT](LICENSE)
