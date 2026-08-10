package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RunMetrics summarizes observability events from .orchestra/llm_log.jsonl.
type RunMetrics struct {
	ValidationErrors int
	ToolCalls        int
	ToolResults      int
	ClassifiedSteps  int
}

type logEntry struct {
	Event string `json:"event"`
	Kind  string `json:"kind"`
}

// ParseLLMLog reads an llm_log.jsonl file and counts eval-relevant events.
func ParseLLMLog(path string) (RunMetrics, error) {
	var m RunMetrics
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, fmt.Errorf("open llm log: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e logEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		switch e.Event {
		case "tool_call":
			m.ToolCalls++
		case "tool_result":
			m.ToolResults++
		case "step.classified":
			m.ClassifiedSteps++
			if e.Kind == "validation_error" {
				m.ValidationErrors++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return m, fmt.Errorf("read llm log: %w", err)
	}
	return m, nil
}
