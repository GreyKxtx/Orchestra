// Package wire is the client↔core contract: the handshake, the names of the
// methods, notifications and requests, the parameters and results of the
// methods, and the payloads of the notifications (ARCH-4).
//
// Every client — the TUI, the CLI's --via-core path, the VS Code extension,
// the web page — used to keep its own copy of these shapes, by hand, and the
// core built its notifications as map[string]any, so nothing held the copies
// together. This package is the one definition: Go clients import it, and
// the core answers and notifies with its types.
//
// It depends on nothing but the standard library and the protocol package,
// so every module can import it.
package wire
