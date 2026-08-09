module github.com/orchestra/orchestra/patch

go 1.25.0

require (
	github.com/aymanbagabas/go-udiff v0.3.1
	github.com/orchestra/orchestra/protocol v0.0.0
	golang.org/x/sys v0.47.0
)

replace github.com/orchestra/orchestra/protocol => ../protocol
