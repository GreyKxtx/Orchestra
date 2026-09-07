package llm

import (
	"context"
	"sync"
	"time"
)

// LocalServer is an OpenAI-compatible inference server found listening on this
// machine, together with the models it advertises.
type LocalServer struct {
	// APIBase is ready to be written into config verbatim, /v1 included.
	APIBase string
	// Models is the server's own list, in its own order. The first entry is
	// what `orchestra init` prefills, because a local server generally
	// advertises the model it has loaded.
	Models []RemoteModel
}

// localProbeTimeout bounds the whole detection, not one candidate. `orchestra
// init` is interactive: a user with no local server must not wait on three
// connection attempts, and a user with one must not out-wait a cold model
// listing.
const localProbeTimeout = 2 * time.Second

// localServerCandidates returns the well-known local inference endpoints, in
// preference order: LM Studio, Ollama, then vLLM / llama.cpp.
//
// The order is the answer when more than one is up — detection must be
// deterministic, or `orchestra init` writes a different config depending on
// which server happened to answer first.
func localServerCandidates() []string {
	return []string{
		"http://localhost:1234",
		"http://localhost:11434",
		"http://localhost:8000",
	}
}

// DetectLocalServer probes the well-known local inference ports and returns the
// highest-priority one that answers with at least one model.
//
// A server that answers but lists nothing is not reported: prefilling an
// api_base with an empty model is worse than the static default it would
// replace.
func DetectLocalServer(ctx context.Context) (LocalServer, bool) {
	return detectLocalServerAt(ctx, localServerCandidates())
}

// detectLocalServerAt is DetectLocalServer over an explicit candidate list.
//
// Candidates are probed concurrently — three sequential connection attempts to
// a machine running nothing is the slow path that would be felt — but the
// result is chosen by list position, so concurrency never leaks into the answer.
func detectLocalServerAt(ctx context.Context, bases []string) (LocalServer, bool) {
	if len(bases) == 0 {
		return LocalServer{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, localProbeTimeout)
	defer cancel()

	found := make([]LocalServer, len(bases))
	var wg sync.WaitGroup
	for i, base := range bases {
		wg.Add(1)
		go func(i int, base string) {
			defer wg.Done()
			apiBase := base + "/v1"
			models, err := ListRemoteModels(ctx, LLMConfig{APIBase: apiBase})
			if err != nil || len(models) == 0 {
				return
			}
			found[i] = LocalServer{APIBase: apiBase, Models: models}
		}(i, base)
	}
	wg.Wait()

	for _, s := range found {
		if s.APIBase != "" {
			return s, true
		}
	}
	return LocalServer{}, false
}
