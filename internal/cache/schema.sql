-- Schema for the SQLite cache backend. Nested collections (comments, labels,
-- assignees) and the optional milestone are stored as JSON text so every field
-- of github.Issue / github.PullRequest round-trips losslessly; scalar fields
-- used for filtering are stored as indexed columns.
-- Times are RFC3339Nano strings; nullable times are SQL NULL.

CREATE TABLE IF NOT EXISTS issues (
    host          TEXT    NOT NULL,
    owner         TEXT    NOT NULL,
    repo          TEXT    NOT NULL,
    number        INTEGER NOT NULL,
    title         TEXT    NOT NULL,
    state         TEXT    NOT NULL,
    author_login  TEXT    NOT NULL,
    assignees     TEXT    NOT NULL,
    labels        TEXT    NOT NULL,
    milestone     TEXT,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL,
    closed_at     TEXT,
    url           TEXT    NOT NULL,
    body          TEXT    NOT NULL,
    comment_count INTEGER NOT NULL,
    comments      TEXT    NOT NULL,
    row_mtime     TEXT    NOT NULL,
    PRIMARY KEY (host, owner, repo, number)
);

CREATE INDEX IF NOT EXISTS idx_issues_state   ON issues(host, owner, repo, state);
CREATE INDEX IF NOT EXISTS idx_issues_author  ON issues(host, owner, repo, author_login);
CREATE INDEX IF NOT EXISTS idx_issues_updated ON issues(host, owner, repo, updated_at);

CREATE TABLE IF NOT EXISTS pull_requests (
    host            TEXT    NOT NULL,
    owner           TEXT    NOT NULL,
    repo            TEXT    NOT NULL,
    number          INTEGER NOT NULL,
    title           TEXT    NOT NULL,
    state           TEXT    NOT NULL,
    is_draft        INTEGER NOT NULL,
    author_login    TEXT    NOT NULL,
    assignees       TEXT    NOT NULL,
    labels          TEXT    NOT NULL,
    milestone       TEXT,
    base_ref_name   TEXT    NOT NULL,
    head_ref_name   TEXT    NOT NULL,
    head_ref_oid    TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    merged_at       TEXT,
    closed_at       TEXT,
    url             TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    comment_count   INTEGER NOT NULL,
    comments        TEXT    NOT NULL,
    review_decision TEXT,
    row_mtime       TEXT    NOT NULL,
    PRIMARY KEY (host, owner, repo, number)
);

CREATE INDEX IF NOT EXISTS idx_prs_state   ON pull_requests(host, owner, repo, state);
CREATE INDEX IF NOT EXISTS idx_prs_author  ON pull_requests(host, owner, repo, author_login);
CREATE INDEX IF NOT EXISTS idx_prs_updated ON pull_requests(host, owner, repo, updated_at);

CREATE TABLE IF NOT EXISTS cache_meta (
    host             TEXT    NOT NULL,
    owner            TEXT    NOT NULL,
    repo             TEXT    NOT NULL,
    cached_at        TEXT    NOT NULL,
    duration         INTEGER NOT NULL,
    complete         INTEGER NOT NULL,
    issue_cursor     TEXT,
    pr_cursor        TEXT,
    issues_cached_at TEXT,
    prs_cached_at    TEXT,
    PRIMARY KEY (host, owner, repo)
);
