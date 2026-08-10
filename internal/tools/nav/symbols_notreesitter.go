//go:build !cgo

package nav

import "context"

func goSymbolsViaTreeSitter(ctx context.Context, src []byte) ([]Symbol, bool) {
	_ = ctx
	_ = src
	return nil, false
}
