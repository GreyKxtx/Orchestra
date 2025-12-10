package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LLMConfig contains LLM API settings
type LLMConfig struct {
	APIBase     string  `yaml:"api_base"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float32 `yaml:"temperature"`
}

// ProjectConfig represents the Orchestra configuration
type ProjectConfig struct {
	ProjectRoot  string    `yaml:"project_root"`
	ExcludeDirs  []string  `yaml:"exclude_dirs"`
	ContextLimit int       `yaml:"context_limit_kb"`
	LLM          LLMConfig `yaml:"llm"`
}

// DefaultConfig creates a default configuration for the project root
func DefaultConfig(projectRoot string) *ProjectConfig {
	return &ProjectConfig{
		ProjectRoot: projectRoot,
		ExcludeDirs: []string{
			".git",
			"node_modules",
			"dist",
			"build",
			".orchestra",
		},
		ContextLimit: 50,
		LLM: LLMConfig{
			APIBase:     "http://localhost:8000/v1",
			Model:       "qwen2.5-coder-7b",
			Temperature: 0.7,
			MaxTokens:   4096,
		},
	}
}

// Load loads configuration from file
func Load(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Save saves configuration to file
func Save(path string, cfg *ProjectConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (c *ProjectConfig) Validate() error {
	if c.ProjectRoot == "" {
		return fmt.Errorf("project_root is required")
	}

	if c.LLM.APIBase == "" {
		return fmt.Errorf("llm.api_base is required")
	}

	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}

	if c.ContextLimit <= 0 {
		return fmt.Errorf("context_limit_kb must be greater than 0")
	}

	return nil
}
