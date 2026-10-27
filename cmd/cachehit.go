package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// recordRetrievalOutcome records whether a read command served a retrieval
// operation from the local cache (a hit) or fell back to the GitHub API (a
// miss). The operation is one of the telemetry.Retrieval* labels, so the
// outcome can later be aggregated into a per-operation hit rate.
func recordRetrievalOutcome(repo *gitremote.Repo, operation string, hit bool) {
	rec := initTelemetry()
	if rec == nil || !rec.Enabled() || repo == nil {
		return
	}
	kind := telemetry.KindCacheMiss
	if hit {
		kind = telemetry.KindCacheHit
	}
	rec.Record(telemetry.Event{
		Timestamp: time.Now(),
		Kind:      kind,
		Operation: operation,
		Host:      repo.Host,
		Repo:      strings.ToLower(repo.Owner + "/" + repo.Name),
		Attempts:  1,
	})
}

// retrievalHitRates loads the per-repository, per-operation hit rates from the
// telemetry database. It returns nil when telemetry is off, has never been
// written, or cannot be read: repo list must never fail because metrics are
// unavailable.
func retrievalHitRates() map[string]telemetry.CacheHitRate {
	store := telemetryStore
	if store == nil {
		path := resolveTelemetryDBPath()
		if _, err := os.Stat(path); err != nil {
			return nil
		}
		opened, err := telemetry.Open(path)
		if err != nil {
			return nil
		}
		defer opened.Close()
		store = opened
	}
	rates, err := store.CacheHitRates()
	if err != nil {
		return nil
	}
	out := make(map[string]telemetry.CacheHitRate, len(rates))
	for _, r := range rates {
		out[hitRateKey(r.Host, r.Repo, r.Operation)] = r
	}
	return out
}

// hitRateKey identifies one repository and retrieval operation in a hit-rate
// index. Repository names are case-insensitive, so the key is lower-case.
func hitRateKey(host, repo, operation string) string {
	return strings.ToLower(host) + "\x00" + strings.ToLower(repo) + "\x00" + operation
}

// hitRateLabel renders a hit rate as a whole percentage followed by the total
// number of recorded retrievals, or a dash when the operation has no recorded
// retrieval for the repository.
func hitRateLabel(rates map[string]telemetry.CacheHitRate, host, owner, repo, operation string) string {
	r, ok := rates[hitRateKey(host, owner+"/"+repo, operation)]
	if !ok {
		return "—"
	}
	total := r.Hits + r.Misses
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%d%% (%d)", hitRatePercent(r.Hits, r.Misses), total)
}

// hitRatePercent returns a hit rate as a whole percentage, or 0 when no
// retrievals were recorded.
func hitRatePercent(hits, misses int) int {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return (hits*100 + total/2) / total
}
