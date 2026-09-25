package core

import (
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

type SessionSearchResult struct {
	Hits []sessionfile.Hit `json:"hits"`
}

// SessionSearch finds messages containing the query across every saved session
// in the workspace.
func (c *Core) SessionSearch(params SessionSearchParams) (*SessionSearchResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	hits, err := sessionfile.Search(c.workspaceRoot, sessionfile.SearchOptions{
		Query:       params.Query,
		Insensitive: params.Insensitive,
		IncludeAll:  params.IncludeAll,
		Limit:       params.Limit,
	})
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}
	return &SessionSearchResult{Hits: hits}, nil
}

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	SessionSearchParams = wire.SessionSearchParams
)
