package git

import (
	"github.com/orchestra/orchestra/internal/tools/toolschema"
	"github.com/orchestra/orchestra/llm"
)
func ToolGitStatus() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.status",
			Description: "Show current git working-tree status — staged/unstaged changes, untracked files, current branch.",
			Parameters:  toolschema.MustSchema(`{"type":"object","additionalProperties":false,"properties":{}}`),
		},
	}
}

func ToolGitLog() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.log",
			Description: "Show commit history. n limits the count (default 20, max 200). Optionally filtered by ref or path.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "n":       { "type": "integer", "minimum": 1, "maximum": 200, "description": "Max commits to show. Default 20." },
    "ref":     { "type": "string", "description": "Branch, tag, or commit hash." },
    "path":    { "type": "string", "description": "Limit to commits touching this workspace-relative path." },
    "oneline": { "type": "boolean", "description": "Compact single-line format." }
  }
}`),
		},
	}
}

func ToolGitCommit() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.commit",
			Description: "Stage files and create a git commit. Use add=[\".\"] to stage all changes.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["message"],
  "properties": {
    "message":     { "type": "string", "minLength": 1, "description": "Commit message." },
    "add":         { "type": "array", "items": {"type":"string"}, "description": "Workspace-relative paths to git add. Use [\".\"] for all changes." },
    "allow_empty": { "type": "boolean", "description": "Allow commit with no staged changes." }
  }
}`),
		},
	}
}

func ToolGitBranch() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.branch",
			Description: "List, create, or delete a local branch. Defaults to listing when no option is set.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "list":   { "type": "boolean", "description": "List local branches (default)." },
    "create": { "type": "string",  "description": "Create a branch with this name." },
    "delete": { "type": "string",  "description": "Delete a branch with this name." }
  }
}`),
		},
	}
}

func ToolGitCheckout() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.checkout",
			Description: "Switch to a branch/commit or restore specific files. new_branch creates and switches (-b).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "ref":        { "type": "string", "description": "Branch, tag, or commit to switch to." },
    "paths":      { "type": "array",  "items": {"type":"string"}, "description": "Workspace-relative paths to restore from HEAD." },
    "new_branch": { "type": "string", "description": "Create this branch and switch to it (-b)." }
  }
}`),
		},
	}
}

func ToolGitPush() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.push",
			Description: "Push current branch to remote. force=true uses --force-with-lease (safer than --force). Default remote is 'origin'.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "remote":       { "type": "string",  "description": "Remote name. Default 'origin'." },
    "branch":       { "type": "string",  "description": "Branch to push. Default: current branch." },
    "set_upstream": { "type": "boolean", "description": "Set upstream tracking (-u)." },
    "force":        { "type": "boolean", "description": "Push with --force-with-lease." }
  }
}`),
		},
	}
}

func ToolGitDiff() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "git.diff",
			Description: "Show diff of uncommitted changes. staged=true shows staged (--cached) changes. ref compares against a specific commit or branch.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "staged": { "type": "boolean", "description": "Show staged (--cached) changes instead of unstaged." },
    "ref":    { "type": "string", "description": "Compare against this commit, branch, or tag." },
    "path":   { "type": "string", "description": "Limit diff to this workspace-relative file or directory." }
  }
}`),
		},
	}
}

func toolBrowserNavigate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.navigate",
			Description: "Открыть URL в браузере и дождаться загрузки страницы.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["url"],
  "properties": {
    "url": { "type": "string", "minLength": 1 },
    "wait_until": { "type": "string", "enum": ["load", "domcontentloaded", "networkidle"] }
  }
}`),
		},
	}
}

func toolBrowserSnapshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.snapshot",
			Description: "Вернуть accessibility-дерево текущей страницы (структурированный текст с ref-идентификаторами для кликов).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func toolBrowserScreenshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.screenshot",
			Description: "Снять скриншот текущей страницы (base64 PNG).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "full_page": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserClick() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.click",
			Description: "Нажать на элемент по имени или ref из snapshot.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserType() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.type",
			Description: "Ввести текст в поле ввода (по имени или ref из snapshot).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["text"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "text": { "type": "string" },
    "clear": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserFill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.fill",
			Description: "Заполнить несколько полей формы за один вызов.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["fields"],
  "properties": {
    "fields": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["value"],
        "properties": {
          "element": { "type": "string" },
          "ref": { "type": "string" },
          "value": { "type": "string" }
        }
      }
    }
  }
}`),
		},
	}
}

func toolBrowserSelect() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.select",
			Description: "Выбрать опцию в выпадающем списке <select>.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["value"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "value": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserEval() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.eval",
			Description: "Выполнить JavaScript в контексте страницы. Требует allow_eval: true в конфигурации.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["expression"],
  "properties": {
    "expression": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolBrowserWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.wait",
			Description: "Ждать условие: совпадение URL, появление CSS-селектора или текста на странице.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "url": { "type": "string" },
    "selector": { "type": "string" },
    "text": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolBrowserClose() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.close",
			Description: "Закрыть текущую страницу (браузер остаётся запущенным для повторного использования).",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func ToolGHPRList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.list",
			Description: "List pull requests in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","merged","all"], "description": "Filter by PR state. Default: open." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max PRs to return. Default: 20." },
    "base":   { "type": "string", "description": "Filter by base branch name." }
  }
}`),
		},
	}
}

func ToolGHPRCreate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.create",
			Description: "Create a pull request from the current branch. Requires gh CLI installed and authenticated.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title"],
  "properties": {
    "title": { "type": "string", "minLength": 1, "description": "PR title." },
    "body":  { "type": "string", "description": "PR description (markdown)." },
    "base":  { "type": "string", "description": "Base branch. Defaults to repo default branch." },
    "draft": { "type": "boolean", "description": "Create as draft PR." }
  }
}`),
		},
	}
}

func ToolGHPRView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.pr.view",
			Description: "View details of a pull request including description and comments. number=0 uses the current branch's PR.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "number": { "type": "integer", "minimum": 0, "description": "PR number. Omit or 0 = current branch's PR." }
  }
}`),
		},
	}
}

func ToolGHIssueList() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.list",
			Description: "List issues in the current GitHub repository. Requires gh CLI installed and authenticated.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "state":  { "type": "string", "enum": ["open","closed","all"], "description": "Filter by state. Default: open." },
    "labels": { "type": "array", "items": { "type": "string" }, "description": "Filter by label names." },
    "limit":  { "type": "integer", "minimum": 1, "maximum": 100, "description": "Max issues to return. Default: 20." }
  }
}`),
		},
	}
}

func ToolGHIssueView() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "gh.issue.view",
			Description: "View details of a GitHub issue including description and comments.",
			Parameters: toolschema.MustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["number"],
  "properties": {
    "number": { "type": "integer", "minimum": 1, "description": "Issue number." }
  }
}`),
		},
	}
}