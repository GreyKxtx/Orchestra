//go:build !cgo

// Stub when CGO is disabled (e.g. some Windows CI/toolchains without a C compiler).

package nav

import "context"

func goSymbolsViaTreeSitter(ctx context.Context, src []byte) ([]Symbol, bool) {
	_ = ctx
	_ = src
	return nil, false
}
