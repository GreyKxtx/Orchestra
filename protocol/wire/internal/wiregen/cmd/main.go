// Command wiregen writes the generated copies of the wire contract. Run by
// go generate from protocol/wire; see wiregen.Generate.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/protocol/wire/internal/wiregen"
)

func main() {
	wireDir := "."
	if len(os.Args) > 1 {
		wireDir = os.Args[1]
	}
	files, err := wiregen.Generate(wireDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiregen:", err)
		os.Exit(1)
	}
	root := filepath.Join(wireDir, "..", "..")
	if err := wiregen.Write(root, files); err != nil {
		fmt.Fprintln(os.Stderr, "wiregen:", err)
		os.Exit(1)
	}
	for rel := range files {
		fmt.Println("wrote", rel)
	}
}
