package llm

import (
	"context"
	"errors"
)

// ErrNoModel is what a client answers when its configuration names no model.
// A project starts with none: the user picks one from the providers they
// connected, and until then there is nothing to send a request to. A guessed
// default sent it somewhere the user never chose — `orchestra init` wrote a
// local qwen2.5-coder-7b, and a project on Anthropic went on asking for it.
var ErrNoModel = errors.New("no model selected: pick a model from your providers in Settings")

// noModelClient is the client of a configuration with no model.
type noModelClient struct{}

func (noModelClient) Complete(context.Context, CompleteRequest) (*CompleteResponse, error) {
	return nil, ErrNoModel
}

func (noModelClient) Plan(context.Context, string) (string, error) {
	return "", ErrNoModel
}

// HasNoModel reports whether the client stack c has no model to call: its
// provider client is the one of a configuration that names none.
func HasNoModel(c Client) bool {
	found := false
	walkStack(c, func(l Client) bool {
		if _, ok := l.(noModelClient); ok {
			found = true
			return true
		}
		return false
	})
	return found
}
