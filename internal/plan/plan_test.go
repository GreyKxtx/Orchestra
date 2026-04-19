package plan

import (
	"testing"
)

func TestParsePlan_ValidJSON(t *testing.T) {
	jsonStr := `{
		"steps": [
			{
				"file_path": "path/to/file.go",
				"action": "modify",
				"summary": "Add new function"
			}
		]
	}`

	p, err := ParsePlan(jsonStr)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	if len(p.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(p.Steps))
	}

	step := p.Steps[0]
	if step.FilePath != "path/to/file.go" {
		t.Errorf("Expected file_path 'path/to/file.go', got '%s'", step.FilePath)
	}
	if step.Action != ActionModify {
		t.Errorf("Expected action 'modify', got '%s'", step.Action)
	}
	if step.Summary != "Add new function" {
		t.Errorf("Expected summary 'Add new function', got '%s'", step.Summary)
	}
}

func TestParsePlan_JSONInMarkdownBlock(t *testing.T) {
	jsonStr := "```json\n{\n  \"steps\": [\n    {\n      \"file_path\": \"file.go\",\n      \"action\": \"create\",\n      \"summary\": \"Create new file\"\n    }\n  ]\n}\n```"

	p, err := ParsePlan(jsonStr)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	if len(p.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(p.Steps))
	}

	step := p.Steps[0]
	if step.FilePath != "file.go" {
		t.Errorf("Expected file_path 'file.go', got '%s'", step.FilePath)
	}
	if step.Action != ActionCreate {
		t.Errorf("Expected action 'create', got '%s'", step.Action)
	}
}

func TestParsePlan_MultipleSteps(t *testing.T) {
	jsonStr := `{
		"steps": [
			{
				"file_path": "a.go",
				"action": "modify",
				"summary": "Update function"
			},
			{
				"file_path": "b.go",
				"action": "create",
				"summary": "Create new file"
			}
		]
	}`

	p, err := ParsePlan(jsonStr)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	if len(p.Steps) != 2 {
		t.Fatalf("Expected 2 steps, got %d", len(p.Steps))
	}

	if p.Steps[0].FilePath != "a.go" {
		t.Errorf("Expected first step file_path 'a.go', got '%s'", p.Steps[0].FilePath)
	}
	if p.Steps[1].FilePath != "b.go" {
		t.Errorf("Expected second step file_path 'b.go', got '%s'", p.Steps[1].FilePath)
	}
}

func TestParsePlan_EmptySteps(t *testing.T) {
	jsonStr := `{
		"steps": []
	}`

	_, err := ParsePlan(jsonStr)
	if err == nil {
		t.Fatal("Expected error for empty steps, got nil")
	}

	if err.Error() != "plan has no steps" {
		t.Errorf("Expected error 'plan has no steps', got '%s'", err.Error())
	}
}

func TestParsePlan_InvalidAction(t *testing.T) {
	jsonStr := `{
		"steps": [
			{
				"file_path": "file.go",
				"action": "unknown_action",
				"summary": "Test"
			}
		]
	}`

	_, err := ParsePlan(jsonStr)
	if err == nil {
		t.Fatal("Expected error for invalid action, got nil")
	}
}

func TestParsePlan_EmptyFilePath(t *testing.T) {
	jsonStr := `{
		"steps": [
			{
				"file_path": "",
				"action": "modify",
				"summary": "Test"
			}
		]
	}`

	_, err := ParsePlan(jsonStr)
	if err == nil {
		t.Fatal("Expected error for empty file_path, got nil")
	}
}

func TestParsePlan_AllActions(t *testing.T) {
	jsonStr := `{
		"steps": [
			{
				"file_path": "modify.go",
				"action": "modify",
				"summary": "Modify file"
			},
			{
				"file_path": "create.go",
				"action": "create",
				"summary": "Create file"
			},
			{
				"file_path": "delete.go",
				"action": "delete",
				"summary": "Delete file"
			}
		]
	}`

	p, err := ParsePlan(jsonStr)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}

	if len(p.Steps) != 3 {
		t.Fatalf("Expected 3 steps, got %d", len(p.Steps))
	}

	if p.Steps[0].Action != ActionModify {
		t.Errorf("Expected first action 'modify', got '%s'", p.Steps[0].Action)
	}
	if p.Steps[1].Action != ActionCreate {
		t.Errorf("Expected second action 'create', got '%s'", p.Steps[1].Action)
	}
	if p.Steps[2].Action != ActionDelete {
		t.Errorf("Expected third action 'delete', got '%s'", p.Steps[2].Action)
	}
}

func TestParsePlan_InvalidJSON(t *testing.T) {
	jsonStr := `{ invalid json }`

	_, err := ParsePlan(jsonStr)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}
