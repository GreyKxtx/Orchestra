package main

import (
	"os"

	"github.com/orchestra/orchestra/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
