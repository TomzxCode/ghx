package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

// benchFixture writes n issues for a single repository into a temp cache and
// returns the store, mirroring the real list-read path (LoadAllIssues then
// filterIssues). This is the Phase 0 baseline for NFR-01.
func benchFixture(b *testing.B, n int) *cache.Store {
	b.Helper()
	store := cache.NewStoreWithPath(b.TempDir())
	for i := 1; i <= n; i++ {
		issue := &github.Issue{
			Number:  i,
			Title:   fmt.Sprintf("issue %d: something about the widget", i),
			State:   map[bool]string{true: "OPEN", false: "CLOSED"}[i%2 == 0],
			Author:  github.Actor{Login: fmt.Sprintf("user%d", i%10)},
			Labels:  []github.Label{{Name: "bug"}, {Name: fmt.Sprintf("area-%d", i%5)}},
			Body:      "body text for the issue, long enough to be realistic",
			UpdatedAt: time.Now(),
		}
		if err := store.SaveIssue("github.com", "acme", "big", issue); err != nil {
			b.Fatalf("seed issue %d: %v", i, err)
		}
	}
	return store
}

func BenchmarkFileBackendListState(b *testing.B) {
	store := benchFixture(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all, err := store.LoadAllIssues("github.com", "acme", "big")
		if err != nil {
			b.Fatal(err)
		}
		_ = filterIssues(all, "open", "", "", nil, "", "", "", "")
	}
}

func BenchmarkFileBackendListAuthor(b *testing.B) {
	store := benchFixture(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all, err := store.LoadAllIssues("github.com", "acme", "big")
		if err != nil {
			b.Fatal(err)
		}
		_ = filterIssues(all, "all", "", "user3", nil, "", "", "", "")
	}
}

func BenchmarkFileBackendSearch(b *testing.B) {
	store := benchFixture(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all, err := store.LoadAllIssues("github.com", "acme", "big")
		if err != nil {
			b.Fatal(err)
		}
		_ = filterIssues(all, "all", "", "", nil, "", "", "", "widget")
	}
}
