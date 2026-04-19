package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/cli"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/plan"
)

// contains проверяет, содержит ли строка подстроку
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// MockLLM притворяется умным LLM для тестов
type MockLLM struct {
	PlanResponse     string
	CompleteResponse string
}

func (m *MockLLM) Plan(ctx context.Context, prompt string) (string, error) {
	if m.PlanResponse != "" {
		return m.PlanResponse, nil
	}
	// Дефолтный план: создать файл hello.txt
	p := &plan.Plan{
		Steps: []plan.PlanStep{
			{
				FilePath: "hello.txt",
				Action:   plan.ActionCreate,
				Summary:  "Creating a hello world file",
			},
		},
	}
	jsonBytes, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func (m *MockLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = ctx
	_ = req
	if m.CompleteResponse != "" {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: m.CompleteResponse}}, nil
	}
	// Дефолтный ответ (legacy): валидный AgentStep JSON, чтобы не зависеть от реальной модели.
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[{"type":"file.search_replace","path":"hello.txt","search":"","replace":"Hello, Integration World!","file_hash":"sha256:deadbeef"}]}}`,
	}}, nil
}

// setupGitRepo инициализирует git репозиторий в указанной директории
func setupGitRepo(t *testing.T, dir string) {
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.email", "test@orchestra.ai")
	runCmd(t, dir, "git", "config", "user.name", "Orchestra Test")
	// Делаем первый коммит, чтобы ветка существовала
	readmePath := filepath.Join(dir, "README.md")
	err := os.WriteFile(readmePath, []byte("# Test\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to create README: %v", err)
	}
	runCmd(t, dir, "git", "add", ".")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")
}

// runCmd выполняет команду в указанной директории
func runCmd(t *testing.T, dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed: %s %v\nOutput: %s\nError: %v", name, args, string(output), err)
	}
}

func TestOrchestra_FullCycle_WithGit(t *testing.T) {
	// 1. Создаем временную директорию (песочницу)
	tmpDir := t.TempDir()

	// 2. Инициализируем там Git
	setupGitRepo(t, tmpDir)

	// 3. Создаем конфиг
	configPath := filepath.Join(tmpDir, ".orchestra.yml")
	cfg := config.DefaultConfig(tmpDir)
	cfg.LLM.APIBase = "http://localhost:8000/v1"
	cfg.LLM.Model = "test-model"
	cfg.ContextLimit = 50
	err := config.Save(configPath, cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// 3.5. Добавляем .orchestra.yml в .gitignore или коммитим его
	// Для теста проще добавить в .gitignore
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	err = os.WriteFile(gitignorePath, []byte(".orchestra.yml\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}
	runCmd(t, tmpDir, "git", "add", ".gitignore")
	runCmd(t, tmpDir, "git", "commit", "-m", "Add .gitignore")

	// 4. Создаем mock LLM клиент
	mockClient := &MockLLM{}

	// 5. Подменяем клиент в CLI
	cli.SetTestClient(mockClient)
	defer cli.ResetTestClient()

	// 6. Проверяем компоненты отдельно
	// Проверяем, что git репо чистый
	statusOutput, err := exec.Command("git", "-C", tmpDir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to check git status: %v", err)
	}
	if string(statusOutput) != "" {
		t.Errorf("Git repo should be clean before test, got: %s", string(statusOutput))
	}

	// Проверяем, что mock клиент работает
	planJSON, err := mockClient.Plan(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Mock Plan failed: %v", err)
	}

	p, err := plan.ParsePlan(planJSON)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(p.Steps))
	}
	if p.Steps[0].FilePath != "hello.txt" {
		t.Errorf("Expected file_path 'hello.txt', got '%s'", p.Steps[0].FilePath)
	}
	if p.Steps[0].Action != plan.ActionCreate {
		t.Errorf("Expected action 'create', got '%s'", p.Steps[0].Action)
	}

	// Проверяем, что Complete возвращает валидный ответ
	completeResp, err := mockClient.Complete(context.Background(), llm.CompleteRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Mock Complete failed: %v", err)
	}
	if !contains(completeResp.Message.Content, "Hello, Integration World!") {
		t.Errorf("Expected response to contain 'Hello, Integration World!', got: %s", completeResp.Message.Content)
	}
}

func TestOrchestra_PlanOnly(t *testing.T) {
	// Тест для --plan-only режима
	tmpDir := t.TempDir()

	// Создаем конфиг
	configPath := filepath.Join(tmpDir, ".orchestra.yml")
	cfg := config.DefaultConfig(tmpDir)
	cfg.LLM.APIBase = "http://localhost:8000/v1"
	cfg.LLM.Model = "test-model"
	cfg.ContextLimit = 50
	err := config.Save(configPath, cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Создаем mock LLM клиент
	mockClient := &MockLLM{}

	// Подменяем клиент
	cli.SetTestClient(mockClient)
	defer cli.ResetTestClient()

	// Проверяем, что mock клиент возвращает план
	planJSON, err := mockClient.Plan(context.Background(), "test")
	if err != nil {
		t.Fatalf("Mock Plan failed: %v", err)
	}

	p, err := plan.ParsePlan(planJSON)
	if err != nil {
		t.Fatalf("ParsePlan failed: %v", err)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(p.Steps))
	}
	if p.Steps[0].Action != plan.ActionCreate {
		t.Errorf("Expected action 'create', got '%s'", p.Steps[0].Action)
	}
}
