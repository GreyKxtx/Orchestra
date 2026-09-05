package usage

import "github.com/orchestra/orchestra/llm"

// UseCatalogPrices installs the built-in models.dev price snapshot as the last
// fallback for cost, but only when apiBase belongs to a hosted provider
// Orchestra ships in its catalogue.
//
// The endpoint check is the whole point and lives here, in one place, so the
// two tracker construction sites (CLI and RPC core) cannot drift: a local
// runtime serving a model that happens to share a hosted model's name must
// keep costing zero, and a self-hosted server behind a public tunnel is not a
// vendor endpoint either.
func (t *Tracker) UseCatalogPrices(apiBase string) {
	if t == nil || !llm.IsKnownCloudEndpoint(apiBase) {
		return
	}
	t.UseListPrices(catalogListPrice)
}

func catalogListPrice(model string) (ModelPricing, bool) {
	in, out, ok := llm.ModelListPrice(model)
	if !ok {
		return ModelPricing{}, false
	}
	return ModelPricing{InputPer1M: in, OutputPer1M: out}, true
}
