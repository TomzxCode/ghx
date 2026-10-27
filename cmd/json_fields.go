package cmd

import (
	"fmt"
	"sort"
	"strings"
)

// resolveJSONFieldList validates a comma-separated --json field list against
// the supported fields and returns the names to emit, deduplicated and in the
// order given. An empty list, or the all sentinel, selects every supported
// field in lexical order.
func resolveJSONFieldList[V any](raw string, supported map[string]V, all string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == all {
		return sortedJSONFieldNames(supported), nil
	}

	var fields []string
	seen := map[string]bool{}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := supported[name]; !ok {
			return nil, fmt.Errorf("unknown JSON field: %q\nAvailable fields:\n  %s",
				name, strings.Join(sortedJSONFieldNames(supported), "\n  "))
		}
		if !seen[name] {
			seen[name] = true
			fields = append(fields, name)
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no JSON fields specified")
	}
	return fields, nil
}

// sortedJSONFieldNames returns every supported field name in lexical order,
// used for the "available fields" error listing.
func sortedJSONFieldNames[V any](supported map[string]V) []string {
	names := make([]string, 0, len(supported))
	for name := range supported {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
