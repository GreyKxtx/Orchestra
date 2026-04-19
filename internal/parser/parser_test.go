package parser

import (
	"testing"
)

func TestParse_SingleFile_ReplaceBlock(t *testing.T) {
	input := `---FILE: path/to/file.go
<<<BLOCK
old code
>>>BLOCK
new code
---END`

	parsed, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(parsed.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(parsed.Files))
	}

	file := parsed.Files[0]
	if file.Path != "path/to/file.go" {
		t.Errorf("Expected path 'path/to/file.go', got '%s'", file.Path)
	}

	if len(file.Operations) != 1 {
		t.Fatalf("Expected 1 operation, got %d", len(file.Operations))
	}

	op := file.Operations[0]
	if op.Type != OpReplaceBlock {
		t.Errorf("Expected OpReplaceBlock, got %v", op.Type)
	}

	if op.OldBlock != "old code" {
		t.Errorf("Expected OldBlock 'old code', got '%s'", op.OldBlock)
	}

	if op.NewBlock != "new code" {
		t.Errorf("Expected NewBlock 'new code', got '%s'", op.NewBlock)
	}
}

func TestParse_SingleFile_ReplaceFile(t *testing.T) {
	input := `---FILE: path/to/file.go
new file content
---END`

	parsed, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(parsed.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(parsed.Files))
	}

	op := parsed.Files[0].Operations[0]
	if op.Type != OpReplaceFile {
		t.Errorf("Expected OpReplaceFile, got %v", op.Type)
	}

	if op.NewFileContent != "new file content" {
		t.Errorf("Expected NewFileContent 'new file content', got '%s'", op.NewFileContent)
	}
}

func TestParse_MultipleFiles(t *testing.T) {
	input := `---FILE: a.go
<<<BLOCK
old1
>>>BLOCK
new1
---END
---FILE: b.go
<<<BLOCK
old2
>>>BLOCK
new2
---END`

	parsed, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(parsed.Files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(parsed.Files))
	}

	if parsed.Files[0].Path != "a.go" {
		t.Errorf("Expected first file 'a.go', got '%s'", parsed.Files[0].Path)
	}

	if parsed.Files[1].Path != "b.go" {
		t.Errorf("Expected second file 'b.go', got '%s'", parsed.Files[1].Path)
	}
}

func TestParse_EmptyOldBlock_ReplaceFile(t *testing.T) {
	input := `---FILE: file.go
<<<BLOCK
>>>BLOCK
new content
---END`

	parsed, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	op := parsed.Files[0].Operations[0]
	if op.Type != OpReplaceFile {
		t.Errorf("Expected OpReplaceFile for empty old block, got %v", op.Type)
	}
}

func TestParse_InvalidFormat_NoFileMarker(t *testing.T) {
	input := `some random text`

	_, err := Parse(input)
	if err == nil {
		t.Fatal("Expected error for invalid format, got nil")
	}
}

func TestParse_InvalidFormat_UnclosedBlock(t *testing.T) {
	input := `---FILE: file.go
<<<BLOCK
old
>>>BLOCK
new
`

	_, err := Parse(input)
	if err == nil {
		t.Fatal("Expected error for unclosed block, got nil")
	}
}

func TestParse_EmptyResponse(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("Expected error for empty response, got nil")
	}
}
