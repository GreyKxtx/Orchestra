package usage

import "testing"

// listPriceStub stands in for llm.ModelListPrice: it knows one model.
func listPriceStub(model string) (ModelPricing, bool) {
	if model == "m-builtin" {
		return ModelPricing{InputPer1M: 3, OutputPer1M: 15}, true
	}
	return ModelPricing{}, false
}

func TestTracker_ListPrice_FillsModelsTheConfigTableOmits(t *testing.T) {
	tr := NewTracker("r", "apply", Pricing{
		"openai": {"m-configured": ModelPricing{InputPer1M: 1, OutputPer1M: 1}},
	})
	tr.UseListPrices(listPriceStub)

	tr.Record("openai", "m-builtin", 1_000_000, 100_000) // $3.00 + $1.50
	_, _, _, _, cost := tr.Total()
	if cost < 4.49 || cost > 4.51 {
		t.Fatalf("cost = %v, want ~4.50 from the built-in table", cost)
	}
}

func TestTracker_ListPrice_LosesToTheUsersOwnTable(t *testing.T) {
	// A user who wrote a pricing table meant it — an enterprise discount, a
	// gateway markup. The snapshot must never override it.
	tr := NewTracker("r", "apply", Pricing{
		"openai": {"m-builtin": ModelPricing{InputPer1M: 1, OutputPer1M: 0}},
	})
	tr.UseListPrices(listPriceStub)

	tr.Record("openai", "m-builtin", 1_000_000, 100_000)
	_, _, _, _, cost := tr.Total()
	if cost < 0.99 || cost > 1.01 {
		t.Fatalf("cost = %v, want ~1.00 from the configured table", cost)
	}
}

func TestTracker_ListPrice_LosesToTheProvidersOwnCost(t *testing.T) {
	tr := NewTracker("r", "apply", nil)
	tr.UseListPrices(listPriceStub)

	tr.RecordCost("openrouter", "m-builtin", 1_000_000, 100_000, 0.25)
	_, _, _, _, cost := tr.Total()
	if cost < 0.249 || cost > 0.251 {
		t.Fatalf("cost = %v, want the provider-reported 0.25", cost)
	}
}

func TestTracker_ListPrice_UnknownModelStaysFree(t *testing.T) {
	tr := NewTracker("r", "apply", nil)
	tr.UseListPrices(listPriceStub)

	tr.Record("lmstudio", "my-local-finetune", 1_000_000, 100_000)
	_, _, _, _, cost := tr.Total()
	if cost != 0 {
		t.Fatalf("cost = %v, want 0 for a model no table knows", cost)
	}
}

func TestTracker_ListPrice_NotInstalledMeansNoCost(t *testing.T) {
	tr := NewTracker("r", "apply", nil)
	tr.Record("lmstudio", "m-builtin", 1_000_000, 100_000)
	_, _, _, _, cost := tr.Total()
	if cost != 0 {
		t.Fatalf("cost = %v, want 0 when no list-price resolver was installed", cost)
	}
}
