package view

import (
	"strings"
	"testing"
)

func TestBuildOrchestraProviderOpts_MainAndCatalog(t *testing.T) {
	ctx := OrchestraDialogCtx{
		Named: map[string]OrchestraNamedProvider{},
	}
	opts := buildOrchestraProviderOpts(ctx)
	if len(opts) < 2 || opts[0].Key != "" || opts[0].Label != "Main" {
		t.Fatalf("want Main first, got %+v", opts)
	}
	foundVLLM := false
	for _, o := range opts {
		if o.Key == "vllm" {
			foundVLLM = true
		}
	}
	if !foundVLLM {
		t.Fatal("vllm should appear in orchestra provider opts")
	}
}

func TestBuildOrchestraProviderOpts_Fast(t *testing.T) {
	ctx := OrchestraDialogCtx{
		FastProvider: "lmstudio",
		Named:        map[string]OrchestraNamedProvider{},
	}
	opts := buildOrchestraProviderOpts(ctx)
	if len(opts) < 2 || opts[1].Key != "lmstudio" || !strings.Contains(opts[1].Label, "Fast") {
		t.Fatalf("fast option: %+v", opts[1])
	}
}

func TestOrchestraRoleStatus_MainNeedsURL(t *testing.T) {
	d := NewOrchestraDialog(nil, OrchestraDialogCtx{
		MainAPIBase: "",
		MainModel:   "m",
		Named:       map[string]OrchestraNamedProvider{},
	})
	ok, detail := d.roleStatus(d.roles[0])
	if ok || detail == "" {
		t.Fatalf("expected main URL error, ok=%v detail=%q", ok, detail)
	}
}

func TestOrchestraDialog_RenderNarrow(t *testing.T) {
	d := NewOrchestraDialog(nil, OrchestraDialogCtx{
		MainAPIBase: "http://localhost:1234",
		MainModel:   "m",
		Named:       map[string]OrchestraNamedProvider{},
	})
	out := d.Render(50, 24)
	if out == "" {
		t.Fatal("empty render")
	}
	// Must not leave raw wrap markers from overflowing fixed columns.
	if strings.Count(out, "tatus") > 0 && !strings.Contains(out, "Status") {
		t.Fatal("Status header looks wrap-broken")
	}
}
