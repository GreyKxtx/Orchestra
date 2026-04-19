//go:build !cgo

package tools

import "context"

func goSymbolsViaTreeSitter(ctx context.Context, src []byte) ([]Symbol, bool) {
	_ = ctx
	_ = src
	return nil, false
}
