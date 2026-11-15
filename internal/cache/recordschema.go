package cache

import (
	"fmt"
	"strings"
	"sync"

	"github.com/tomzxcode/ghx/internal/logging"
)

// Record schema versions. Each cached data kind carries its own version so a
// change to one record shape leaves the other kind serviceable: bumping
// PRSchemaVersion does not invalidate cached issues, and the other way round.
//
// Bump a kind's version when a change to what ghx stores for that kind makes
// records written by an earlier ghx incomplete or misleading: a field the
// reader or an output depends on was added, a field was removed, or a stored
// value changed meaning. Every record is stamped with the version that wrote
// it, so a mismatch marks exactly the records that predate the change.
const (
	// IssueSchemaVersion is the record schema version ghx writes for cached
	// issues.
	IssueSchemaVersion = 1
	// PRSchemaVersion is the record schema version ghx writes for cached pull
	// requests. It moves independently of IssueSchemaVersion.
	PRSchemaVersion = 1
)

// recordSchemaVersionKey is the field a file-backed record carries to name the
// schema version that wrote it. It sits beside the record's own fields, so the
// file stays one JSON object per item.
const recordSchemaVersionKey = "_schemaVersion"

// RecordSchemaVersion returns the record schema version this binary writes for
// one item kind (KindIssues or KindPRs). An unknown kind returns 0.
func RecordSchemaVersion(kind string) int {
	switch kind {
	case KindIssues:
		return IssueSchemaVersion
	case KindPRs:
		return PRSchemaVersion
	}
	return 0
}

// recordTable maps an item kind to the table that stores it.
func recordTable(kind string) (string, error) {
	switch kind {
	case KindIssues:
		return "issues", nil
	case KindPRs:
		return "pull_requests", nil
	}
	return "", fmt.Errorf("unknown record kind %q: use %q or %q", kind, KindIssues, KindPRs)
}

// SchemaStatus describes the record schema versions found in a repository's
// cached records of one kind.
type SchemaStatus struct {
	// Current is the record schema version this ghx writes for the kind.
	Current int
	// Outdated counts cached records written by a version other than Current.
	Outdated int
	// Unversioned counts cached records that carry no version, for example
	// records written before ghx stamped one. A missing stamp is not evidence
	// that a record matches the current schema, so it counts as a mismatch.
	Unversioned int
	// Oldest is the lowest recorded version among the outdated records, or 0
	// when none are outdated.
	Oldest int
}

// Matches reports whether every cached record of the kind was written with the
// current schema, which requires carrying a version that names it.
func (s SchemaStatus) Matches() bool { return s.Outdated == 0 && s.Unversioned == 0 }

func (s *SchemaStatus) add(version, count int) {
	switch {
	case version == 0:
		s.Unversioned += count
	case version == s.Current:
		return
	default:
		s.Outdated += count
		if s.Oldest == 0 || version < s.Oldest {
			s.Oldest = version
		}
	}
}

// SchemaPolicy decides what a read does when cached records were written by the
// record schema of another ghx.
type SchemaPolicy string

const (
	// SchemaServe returns cached records even when their schema differs from
	// the one this ghx writes, and warns that they may not match it. It makes no
	// extra API calls, so it is the default.
	SchemaServe SchemaPolicy = "serve"
	// SchemaRefetch treats records written by another schema version as a cache
	// miss and fetches them from GitHub, so reads return records of the current
	// schema at the cost of API calls.
	SchemaRefetch SchemaPolicy = "refetch"
)

// ParseSchemaPolicy converts a flag or environment value into a policy. An
// empty value is SchemaServe, so an unset setting keeps reads offline.
func ParseSchemaPolicy(value string) (SchemaPolicy, error) {
	switch policy := SchemaPolicy(strings.ToLower(strings.TrimSpace(value))); policy {
	case "", SchemaServe:
		return SchemaServe, nil
	case SchemaRefetch:
		return SchemaRefetch, nil
	default:
		return "", fmt.Errorf("invalid on-schema-change %q: use serve or refetch", value)
	}
}

// String returns the policy as the flag accepts it.
func (p SchemaPolicy) String() string {
	if p == SchemaRefetch {
		return string(SchemaRefetch)
	}
	return string(SchemaServe)
}

// schemaWarned records the repositories already warned about in this process,
// so a long-running server reports a mismatched cache once per repository and
// kind instead of once per request.
var schemaWarned sync.Map

// CachedRecordsVerdict reports whether cached records of kind may be served
// under policy, along with the schema status it was judged from. Records
// written by another schema version make reads fall back to the API under
// SchemaRefetch; under SchemaServe they are served and a warning names the
// mismatch and the way to refetch. A store error never blocks a read, so an
// unreadable status leaves the cache usable.
func CachedRecordsVerdict(store Store, host, owner, repo, kind string, policy SchemaPolicy) (bool, SchemaStatus) {
	if store == nil {
		return true, SchemaStatus{}
	}
	status, err := store.RecordSchemaStatus(host, owner, repo, kind)
	if err != nil {
		logging.Debug("reading record schema status", "repo", repoKey(host, owner, repo), "kind", kind, "error", err)
		return true, status
	}
	if status.Matches() {
		return true, status
	}
	warnSchemaMismatch(host, owner, repo, kind, status, policy)
	return policy != SchemaRefetch, status
}

// CanServeCachedRecords is CachedRecordsVerdict for a caller that only needs the
// answer.
func CanServeCachedRecords(store Store, host, owner, repo, kind string, policy SchemaPolicy) bool {
	serve, _ := CachedRecordsVerdict(store, host, owner, repo, kind, policy)
	return serve
}

// SchemaWarning describes cached records that do not carry the current record
// schema in one sentence, including the fix, or returns "" when every record
// matches.
func SchemaWarning(kind string, status SchemaStatus) string {
	if status.Matches() {
		return ""
	}
	return fmt.Sprintf("cached %s %s and this ghx writes %d, so they may not match this version's fields; run \"ghx cache --type %s --force\" or set --on-schema-change=refetch to refetch them",
		schemaKindLabel(kind), describeRecordVersions(status), status.Current, kind)
}

// describeRecordVersions names the versions a mismatched cache holds, so a
// warning states what the records carry instead of only that they differ.
func describeRecordVersions(status SchemaStatus) string {
	switch {
	case status.Outdated > 0 && status.Unversioned > 0:
		return fmt.Sprintf("use record schema %d (%d have no recorded schema version)", status.Oldest, status.Unversioned)
	case status.Outdated > 0:
		return fmt.Sprintf("use record schema %d", status.Oldest)
	default:
		return "have no recorded schema version"
	}
}

// warnSchemaMismatch emits the mismatch warning once per repository and kind in
// this process. The message names the versions, the number of affected records,
// and the fix, because a warning the user cannot act on is not worth printing.
func warnSchemaMismatch(host, owner, repo, kind string, status SchemaStatus, policy SchemaPolicy) {
	key := kind + "\x00" + repoKey(host, owner, repo)
	if _, warned := schemaWarned.LoadOrStore(key, true); warned {
		return
	}
	message := SchemaWarning(kind, status)
	if policy == SchemaRefetch {
		message = fmt.Sprintf("cached %s %s; refetching them from GitHub (this ghx writes schema %d)",
			schemaKindLabel(kind), describeRecordVersions(status), status.Current)
	}
	logging.Warn(message,
		"repo", repoKey(host, owner, repo), "kind", kind, "outdated", status.Outdated,
		"unversioned", status.Unversioned, "oldest_schema", status.Oldest,
		"current_schema", status.Current, "policy", policy.String())
}

// repoKey formats repository coordinates for a log line.
func repoKey(host, owner, repo string) string {
	return host + "/" + owner + "/" + repo
}

// schemaKindLabel names an item kind in prose.
func schemaKindLabel(kind string) string {
	if kind == KindPRs {
		return "pull requests"
	}
	return "issues"
}
