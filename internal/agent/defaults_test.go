package agent

import "testing"

func TestRetryLimitsForProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		wantInv  int
	}{
		{"anthropic", 1},
		{"openai", 1},
		{"claude-sonnet", 1},
		{"local", 5},
		{"lmstudio", 5},
		{"", 5},
	}
	for _, tc := range cases {
		lim := RetryLimitsForProvider(tc.provider)
		if lim.MaxInvalidRetries != tc.wantInv {
			t.Errorf("RetryLimitsForProvider(%q).MaxInvalidRetries=%d want %d", tc.provider, lim.MaxInvalidRetries, tc.wantInv)
		}
	}
}

func TestFillRetryLimits_RespectsExplicitConfig(t *testing.T) {
	opts := Options{
		MaxInvalidRetries:    9,
		MaxDeniedToolRepeats: 1,
		MaxToolErrorRepeats:  2,
		MaxFinalFailures:     3,
	}
	FillRetryLimits(&opts, "anthropic")
	if opts.MaxInvalidRetries != 9 || opts.MaxDeniedToolRepeats != 1 ||
		opts.MaxToolErrorRepeats != 2 || opts.MaxFinalFailures != 3 {
		t.Fatalf("FillRetryLimits overwrote explicit limits: %+v", opts)
	}
}

func TestFillRetryLimits_ProviderAuto(t *testing.T) {
	opts := Options{}
	FillRetryLimits(&opts, "anthropic")
	if opts.MaxInvalidRetries != 1 {
		t.Fatalf("anthropic auto MaxInvalidRetries=%d want 1", opts.MaxInvalidRetries)
	}
	opts = Options{}
	FillRetryLimits(&opts, "local")
	if opts.MaxInvalidRetries != 5 {
		t.Fatalf("local auto MaxInvalidRetries=%d want 5", opts.MaxInvalidRetries)
	}
}
