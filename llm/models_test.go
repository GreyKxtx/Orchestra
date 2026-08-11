package llm

import "testing"

func TestRemoteModel_ContextTokens(t *testing.T) {
	tests := []struct {
		name string
		m    RemoteModel
		want int
	}{
		{"max_model_len", RemoteModel{MaxModelLen: 8192}, 8192},
		{"max_context_length", RemoteModel{MaxContextLength: 32768}, 32768},
		{"context_length", RemoteModel{ContextLength: 4096}, 4096},
		{"priority", RemoteModel{MaxModelLen: 100, MaxContextLength: 200, ContextLength: 300}, 100},
		{"none", RemoteModel{ID: "x"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.ContextTokens(); got != tt.want {
				t.Fatalf("ContextTokens()=%d want %d", got, tt.want)
			}
		})
	}
}
