package agent

import (
	"testing"
	"time"
)

func TestApplyProfile_Fast(t *testing.T) {
	opts := Options{}
	if err := ApplyProfile(&opts, ProfileFast, false); err != nil {
		t.Fatal(err)
	}
	if opts.MaxSteps != 10 {
		t.Fatalf("MaxSteps=%d", opts.MaxSteps)
	}
	if opts.MaxPromptBytes != 32*1024 {
		t.Fatalf("MaxPromptBytes=%d", opts.MaxPromptBytes)
	}
	if opts.LLMStepTimeout != 60*time.Second {
		t.Fatalf("timeout=%v", opts.LLMStepTimeout)
	}
	if opts.AllowBrowser {
		t.Fatal("fast should disable browser")
	}
	if opts.Profile != ProfileFast {
		t.Fatalf("Profile=%q", opts.Profile)
	}
}

func TestApplyProfile_Precision(t *testing.T) {
	opts := Options{}
	if err := ApplyProfile(&opts, ProfilePrecision, false); err != nil {
		t.Fatal(err)
	}
	if opts.MaxSteps != 36 {
		t.Fatalf("MaxSteps=%d", opts.MaxSteps)
	}
	if opts.MaxPromptBytes != 128*1024 {
		t.Fatalf("MaxPromptBytes=%d", opts.MaxPromptBytes)
	}
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json_schema" {
		t.Fatalf("expected json_schema ResponseFormat, got %#v", opts.ResponseFormat)
	}
}

func TestApplyProfile_PreserveNonZero(t *testing.T) {
	opts := Options{MaxSteps: 99}
	if err := ApplyProfile(&opts, ProfileFast, true); err != nil {
		t.Fatal(err)
	}
	if opts.MaxSteps != 99 {
		t.Fatalf("expected preserved MaxSteps=99, got %d", opts.MaxSteps)
	}
	if opts.MaxPromptBytes != 32*1024 {
		t.Fatalf("zero MaxPromptBytes should be filled, got %d", opts.MaxPromptBytes)
	}
}

func TestApplyProfile_Unknown(t *testing.T) {
	opts := Options{}
	if err := ApplyProfile(&opts, "turbo", false); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyProfile_Empty(t *testing.T) {
	opts := Options{MaxSteps: 5}
	if err := ApplyProfile(&opts, "", false); err != nil {
		t.Fatal(err)
	}
	if opts.MaxSteps != 5 {
		t.Fatal("empty profile must be no-op")
	}
}

func TestIsKnownProfile(t *testing.T) {
	if !IsKnownProfile("") || !IsKnownProfile("fast") || !IsKnownProfile("PRECISION") {
		t.Fatal("expected known")
	}
	if IsKnownProfile("nope") {
		t.Fatal("expected unknown")
	}
}
