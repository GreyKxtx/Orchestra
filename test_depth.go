package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func main() {
	tests := []string{".", "main.go", "internal/resolver", "internal/agent/agent.go"}
	for _, rel := range tests {
		parts := strings.Split(rel, string(filepath.Separator))
		depth := len(parts)
		fmt.Printf("rel=%-25s parts=%-20v depth=%d\n", rel, parts, depth)
	}
}
