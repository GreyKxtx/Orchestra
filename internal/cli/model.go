package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Управление моделью — выбор, контекст, статус",
}

var modelSelectCmd = &cobra.Command{
	Use:   "select",
	Short: "Интерактивный выбор модели и контекста из LM Studio",
	RunE:  runModelSelect,
}

var modelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Показать текущую модель и параметры",
	RunE:  runModelStatus,
}

var modelLoadFlag bool

func init() {
	modelSelectCmd.Flags().BoolVar(&modelLoadFlag, "load", true, "запустить lms load после выбора")
	modelCmd.AddCommand(modelSelectCmd)
	modelCmd.AddCommand(modelStatusCmd)
	rootCmd.AddCommand(modelCmd)
}

func runModelStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}

	fmt.Printf("Модель:   %s\n", cfg.LLM.Model)
	fmt.Printf("API base: %s\n", cfg.LLM.APIBase)

	numCtx := extraBodyNumCtx(cfg)
	if numCtx > 0 {
		fmt.Printf("num_ctx:  %d токенов\n", numCtx)
	} else {
		fmt.Printf("num_ctx:  не задан (LM Studio использует дефолт)\n")
	}
	return nil
}

func runModelSelect(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}

	fmt.Printf("Получаю список моделей из %s...\n", cfg.LLM.APIBase)
	models, err := tui.FetchModels(cfg.LLM.APIBase, cfg.LLM.APIKey)
	if err != nil {
		return fmt.Errorf("не удалось получить список моделей: %w\n\nПроверь что LM Studio запущен и API включён.", err)
	}
	if len(models) == 0 {
		return fmt.Errorf("LM Studio не вернул ни одной модели. Загрузи модель в LM Studio.")
	}

	currentCtx := extraBodyNumCtx(cfg)
	if currentCtx == 0 {
		currentCtx = 4096
	}

	result, err := tui.RunModelPicker(models, cfg.LLM.Model, currentCtx)
	if err != nil {
		return err
	}
	if result.Cancelled {
		fmt.Println("Отменено.")
		return nil
	}

	// Update .orchestra.yml
	if err := updateModelInConfig(cfg.ProjectRoot, result.Model, result.NumCtx); err != nil {
		return fmt.Errorf("не удалось обновить .orchestra.yml: %w", err)
	}
	fmt.Printf("\n✅ Сохранено: model=%s  num_ctx=%d\n", result.Model, result.NumCtx)

	// Optionally call lms load
	if modelLoadFlag {
		if err := lmsLoad(result.Model, result.NumCtx); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  lms load не сработал (%v) — перезагрузи модель в LM Studio вручную.\n", err)
		} else {
			fmt.Printf("✅ lms load выполнен — модель загружена с num_ctx=%d\n", result.NumCtx)
		}
	}
	return nil
}

// loadProjectConfig reads .orchestra.yml from cwd (same logic as other commands).
func loadProjectConfig() (*config.ProjectConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(cwd, ".orchestra.yml")
	return config.Load(cfgPath)
}

// extraBodyNumCtx reads num_ctx from cfg.LLM.ExtraBody (map[string]any).
func extraBodyNumCtx(cfg *config.ProjectConfig) int {
	if cfg.LLM.ExtraBody == nil {
		return 0
	}
	v, ok := cfg.LLM.ExtraBody["num_ctx"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// updateModelInConfig rewrites the model and num_ctx fields in .orchestra.yml.
func updateModelInConfig(projectRoot, model string, numCtx int) error {
	cfgPath := filepath.Join(projectRoot, ".orchestra.yml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}

	// Walk the YAML tree and update llm.model and llm.extra_body.num_ctx.
	updateYAMLNode(&doc, model, numCtx)

	out, err := yaml.Marshal(doc.Content[0])
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, out, 0o600)
}

// updateYAMLNode walks a yaml.Node tree and patches model + num_ctx in place.
func updateYAMLNode(node *yaml.Node, model string, numCtx int) {
	if node.Kind == yaml.DocumentNode {
		for _, c := range node.Content {
			updateYAMLNode(c, model, numCtx)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]

		if key.Value == "llm" && val.Kind == yaml.MappingNode {
			patchLLMNode(val, model, numCtx)
		}
	}
}

func patchLLMNode(llmNode *yaml.Node, model string, numCtx int) {
	numCtxStr := fmt.Sprintf("%d", numCtx)

	// Find and update model; find extra_body.num_ctx.
	extraBodyIdx := -1
	for i := 0; i+1 < len(llmNode.Content); i += 2 {
		key := llmNode.Content[i]
		val := llmNode.Content[i+1]

		if key.Value == "model" {
			val.Value = model
		}
		if key.Value == "extra_body" {
			extraBodyIdx = i + 1
			patchExtraBodyNumCtx(val, numCtxStr)
		}
	}

	// If extra_body didn't exist, add it.
	if extraBodyIdx == -1 {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "extra_body"}
		valNode := &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "num_ctx"},
				{Kind: yaml.ScalarNode, Value: numCtxStr, Tag: "!!int"},
			},
		}
		llmNode.Content = append(llmNode.Content, keyNode, valNode)
	}
}

func patchExtraBodyNumCtx(extraBody *yaml.Node, numCtxStr string) {
	if extraBody.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(extraBody.Content); i += 2 {
		if extraBody.Content[i].Value == "num_ctx" {
			extraBody.Content[i+1].Value = numCtxStr
			return
		}
	}
	// num_ctx not found in extra_body — add it.
	extraBody.Content = append(extraBody.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "num_ctx"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: numCtxStr, Tag: "!!int"},
	)
}

// lmsLoad runs "lms load <model> --context-length <numCtx>".
func lmsLoad(model string, numCtx int) error {
	// lms is the LM Studio CLI tool.
	lmsPath, err := exec.LookPath("lms")
	if err != nil {
		return fmt.Errorf("lms не найден в PATH")
	}
	ctxStr := fmt.Sprintf("%d", numCtx)
	// Strip path prefix if model ID contains slashes (LM Studio uses just the model name).
	modelName := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		modelName = model[idx+1:]
	}
	c := exec.Command(lmsPath, "load", modelName, "--context-length", ctxStr)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
