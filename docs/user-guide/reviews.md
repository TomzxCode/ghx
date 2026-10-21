# Reviews

ghx manages the pull-request review workflow: viewing review threads, staging comments in a pending review, and saving pending comments to a local stash (git-stash style) so you can switch context without losing them.

## View review threads

```bash
ghx pr threads 10                          # open threads, with comments
ghx pr threads 10 --thread <thread-id>     # show a single thread
ghx pr threads 10 --ids                    # include comment IDs
ghx pr threads 10 --state all
ghx pr threads 10 --state resolved
```

Each thread lists its file and line location, followed by every comment in the thread.
Review threads are the inline code comments; top-level PR comments are shown by `ghx pr view <number> --comments`.

## Resolve review threads

```bash
ghx pr threads resolve   <thread-id>             # mark a thread as resolved
ghx pr threads resolve   <id1> <id2> <id3>       # resolve several threads
ghx pr threads unresolve <thread-id>             # reopen a resolved thread
ghx pr threads unresolve <id1> <id2>             # reopen several threads
```

Thread IDs come from `ghx pr threads <number> --ids`.
Every thread in the list is attempted; if some fail, the command reports each failure and exits non-zero.

## Pending reviews

A pending review holds inline and reply comments on GitHub without posting them, so you can batch them into a single review event.

### Start a pending review

```bash
ghx pr review create 10
```

### Add comments to the pending review

Use `pr comment --pending` for each inline or reply comment (see [Comments](comments.md)):

```bash
ghx pr comment 10 --file src/main.go --line 42 --body "Nit" --pending
ghx pr comment 10 --reply-thread <thread-id> --body "Agreed" --pending
```

### List pending reviews

```bash
ghx pr review list 10
```

### Submit the pending review

```bash
ghx pr review submit 10 --event COMMENT
ghx pr review submit 10 --event APPROVE --body "LGTM"
ghx pr review submit 10 --event REQUEST_CHANGES --body "See comments"
ghx pr review submit 10 --review <review-id>   # submit a specific review
```

`--event` accepts `COMMENT`, `APPROVE`, or `REQUEST_CHANGES` (default `COMMENT`).
With no `--review`, your current pending review is submitted.

### Discard a pending review

```bash
ghx pr review discard <review-id>
```

## Review-comment stashes

Stashes save an entire pending review's comments to local disk and delete the pending review from GitHub, so you can restore them later into a new pending review.
Stashes behave like `git stash` and live at `~/.cache/ghx/stash/<owner>/<repo>/<pr>/`.

### Save a pending review to a stash

```bash
ghx pr review stash push 10 -m "nit comments"   # saves to stash@{0}
```

This saves all comments from your current pending review, then deletes that review from GitHub.

### List stash entries

```bash
ghx pr review stash list 10
```

### Restore a stash

```bash
ghx pr review stash pop 10             # restore stash@{0} into a new pending review
ghx pr review stash pop 10 --stash 1   # restore stash@{1}
```

There must be no existing pending review on the PR before popping.
The popped entry is removed from the stash after a successful restore.

### Drop a stash entry

```bash
ghx pr review stash drop 10             # drop stash@{0}
ghx pr review stash drop 10 --stash 1   # drop stash@{1}
```

You can also append a single comment directly to a stash with `pr comment --stash`, without first creating a pending review (see [Comments](comments.md)).
