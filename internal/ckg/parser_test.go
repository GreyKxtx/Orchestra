package ckg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLanguageFromExt(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".py", "python"},
		{".ts", "typescript"},
		{".tsx", "typescript"},
		{".js", "javascript"},
		{".jsx", "javascript"},
		{".java", "java"},
		{".rs", "rust"},
		{".c", "c"},
		{".cpp", "cpp"},
		{".md", "unknown"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		got := LanguageFromExt(tc.ext)
		if got != tc.want {
			t.Errorf("LanguageFromExt(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

func TestParseFile(t *testing.T) {
	// Create a temporary Go file
	tempDir, err := os.MkdirTemp("", "ckg_parser_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	src := `package main

import "fmt"

// Engine defines the core interface
type Engine interface {
	Start() error
	Stop() error
}

// Car is a struct implementation
type Car struct {
	engine Engine
}

// NewCar creates a car
func NewCar(e Engine) *Car {
	return &Car{engine: e}
}

// Drive is a method on Car
func (c *Car) Drive() {
	fmt.Println("Driving...")
}
`
	filePath := filepath.Join(tempDir, "sample.go")
	if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	nodes, _, _, err := ParseFile(context.Background(), "", tempDir, filePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Filter out the synthetic package node added by ParseFile.
	var defNodes []Node
	for _, n := range nodes {
		if n.Kind != "package" {
			defNodes = append(defNodes, n)
		}
	}

	if len(defNodes) != 4 {
		t.Fatalf("Expected 4 definition nodes, got %d: %+v", len(defNodes), defNodes)
	}

	// Helper function to find a node by ShortName.
	findNode := func(name string) *Node {
		for i, n := range defNodes {
			if n.ShortName == name {
				return &defNodes[i]
			}
		}
		return nil
	}

	// 1. Check interface
	engineNode := findNode("Engine")
	if engineNode == nil || engineNode.Kind != "interface" {
		t.Errorf("Engine node missing or invalid: %+v", engineNode)
	} else if engineNode.LineStart != 6 || engineNode.LineEnd != 9 {
		t.Errorf("Engine node has wrong coordinates: %d-%d", engineNode.LineStart, engineNode.LineEnd)
	}

	// 2. Check struct
	carNode := findNode("Car")
	if carNode == nil || carNode.Kind != "struct" {
		t.Errorf("Car node missing or invalid: %+v", carNode)
	} else if carNode.LineStart != 12 || carNode.LineEnd != 14 {
		t.Errorf("Car node has wrong coordinates: %d-%d", carNode.LineStart, carNode.LineEnd)
	}

	// 3. Check func
	newCarNode := findNode("NewCar")
	if newCarNode == nil || newCarNode.Kind != "func" {
		t.Errorf("NewCar node missing or invalid: %+v", newCarNode)
	} else if newCarNode.LineStart != 17 || newCarNode.LineEnd != 19 {
		t.Errorf("NewCar node has wrong coordinates: %d-%d", newCarNode.LineStart, newCarNode.LineEnd)
	}

	// 4. Check method
	driveNode := findNode("Car.Drive")
	if driveNode == nil || driveNode.Kind != "method" {
		t.Errorf("Car.Drive node missing or invalid: %+v", driveNode)
	} else if driveNode.LineStart != 22 || driveNode.LineEnd != 24 {
		t.Errorf("Car.Drive node has wrong coordinates: %d-%d", driveNode.LineStart, driveNode.LineEnd)
	}
}
