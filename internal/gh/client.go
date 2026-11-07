package gh

import (
	"encoding/json"
	"sync"

	"github.com/tomzxcode/ghx/internal/github"
)

var (
	clientMu sync.Mutex
	client   *github.Client
)

// SetClient replaces the GitHub client used by this package. The CLI sets it
// to the client for the target host, and tests set it to a client pointed at a
// local server. Passing nil restores the github.com default.
func SetClient(c *github.Client) {
	clientMu.Lock()
	client = c
	clientMu.Unlock()
}

// graphQLClient returns the configured client, creating one for github.com on
// first use when none was set.
func graphQLClient() (*github.Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	if client != nil {
		return client, nil
	}
	c, err := github.NewClient("github.com")
	if err != nil {
		return nil, err
	}
	client = c
	return c, nil
}

// GraphQL executes a query or mutation and returns the raw data field.
// Requests go through the shared client, which retries rate limits and
// transient server errors.
func GraphQL(query string, variables map[string]interface{}) (json.RawMessage, error) {
	c, err := graphQLClient()
	if err != nil {
		return nil, err
	}
	return c.QueryRaw(query, variables)
}
