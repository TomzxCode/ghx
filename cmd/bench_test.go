package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

const (
	benchHost  = "github.com"
	benchOwner = "acme"
	benchRepo  = "big"
	benchItems = 500
)

func makeBenchIssue(i int) *github.Issue {
	state := "OPEN"
	if i%2 == 0 {
		state = "CLOSED"
	}
	return &github.Issue{
		Number:    i,
		Title:     fmt.Sprintf("issue %d: something about the widget", i),
		State:     state,
		Author:    github.Actor{Login: fmt.Sprintf("user%d", i%10)},
		Labels:    []github.Label{{Name: "bug"}, {Name: fmt.Sprintf("area-%d", i%5)}},
		Body:      "body text for the issue, long enough to be realistic",
		UpdatedAt: time.Now(),
	}
}

func seedIssues(b *testing.B, store cache.Store, n int) {
	b.Helper()
	for i := 1; i <= n; i++ {
		if err := store.SaveIssue(benchHost, benchOwner, benchRepo, makeBenchIssue(i)); err != nil {
			b.Fatalf("seed issue %d: %v", i, err)
		}
	}
}

func benchFileFixture(b *testing.B, n int) cache.Store {
	b.Helper()
	store := cache.NewStoreWithPath(b.TempDir())
	seedIssues(b, store, n)
	return store
}

func benchSQLiteFixture(b *testing.B, n int) cache.Store {
	b.Helper()
	store, err := cache.NewSQLiteStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewSQLiteStore: %v", err)
	}
	b.Cleanup(func() { store.Close() })
	seedIssues(b, store, n)
	return store
}

func benchmarkList(b *testing.B, store cache.Store, q cache.IssueQuery) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.QueryIssues(benchHost, benchOwner, benchRepo, q); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFileBackendListState(b *testing.B) {
	benchmarkList(b, benchFileFixture(b, benchItems), cache.IssueQuery{State: "open"})
}

func BenchmarkFileBackendListAuthor(b *testing.B) {
	benchmarkList(b, benchFileFixture(b, benchItems), cache.IssueQuery{State: "all", Author: "user3"})
}

func BenchmarkFileBackendSearch(b *testing.B) {
	benchmarkList(b, benchFileFixture(b, benchItems), cache.IssueQuery{State: "all", Search: "widget"})
}

func BenchmarkSQLiteBackendListState(b *testing.B) {
	benchmarkList(b, benchSQLiteFixture(b, benchItems), cache.IssueQuery{State: "open"})
}

func BenchmarkSQLiteBackendListAuthor(b *testing.B) {
	benchmarkList(b, benchSQLiteFixture(b, benchItems), cache.IssueQuery{State: "all", Author: "user3"})
}

func BenchmarkSQLiteBackendSearch(b *testing.B) {
	benchmarkList(b, benchSQLiteFixture(b, benchItems), cache.IssueQuery{State: "all", Search: "widget"})
}
