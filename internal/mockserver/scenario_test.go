package mockserver

import (
	"testing"
	"time"
)

// TestScenarioBuilderTimestampWithinAge guards the unit conversion in AddIssue
// and AddPR: updatedAt must never precede createdAt and must stay within the
// item's age.
func TestScenarioBuilderTimestampWithinAge(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ages := []time.Duration{
		0,
		500 * time.Millisecond,
		time.Second,
		time.Minute,
		8 * 24 * time.Hour,
		30 * 24 * time.Hour,
	}

	for _, age := range ages {
		for seed := int64(0); seed < 10; seed++ {
			scenario := NewScenarioBuilder("acme", "demo").
				WithNow(now).
				WithSeed(seed).
				AddIssue("issue", "body", age).
				AddPR("pr", "body", "feat/test", age).
				Build()

			if len(scenario.Issues) != 1 || len(scenario.PRs) != 1 {
				t.Fatalf("age=%s seed=%d: expected one issue and one PR", age, seed)
			}

			checkScenarioTimestamp(t, "issue", scenario.Issues[0].CreatedAt, scenario.Issues[0].UpdatedAt, age, seed)
			checkScenarioTimestamp(t, "PR", scenario.PRs[0].CreatedAt, scenario.PRs[0].UpdatedAt, age, seed)
		}
	}
}

func checkScenarioTimestamp(t *testing.T, kind string, created, updated time.Time, age time.Duration, seed int64) {
	t.Helper()

	drift := updated.Sub(created)
	if drift < 0 || drift > age {
		t.Errorf("%s seed=%d age=%s: updatedAt - createdAt = %s, want within [0, %s]", kind, seed, age, drift, age)
	}
}
