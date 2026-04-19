package schema

// JSON Schemas (draft-07) used for schema enforcement of LLM JSON-only outputs.

const planSchemaJSON = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["steps"],
  "properties": {
    "steps": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["file_path", "action"],
        "properties": {
          "file_path": { "type": "string", "minLength": 1 },
          "action": { "type": "string", "enum": ["modify", "create", "delete"] },
          "summary": { "type": "string" }
        }
      }
    }
  }
}`

const externalPatchesSchemaJSON = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["patches"],
  "properties": {
    "patches": {
      "type": "array",
      "minItems": 1,
      "items": { "$ref": "#/$defs/patch" }
    }
  },
  "$defs": {
    "patch": {
      "oneOf": [
        { "$ref": "#/$defs/file_search_replace" },
        { "$ref": "#/$defs/file_unified_diff" },
        { "$ref": "#/$defs/file_write_atomic" }
      ]
    },
    "file_search_replace": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "search", "replace", "file_hash"],
      "properties": {
        "type": { "const": "file.search_replace" },
        "path": { "type": "string", "minLength": 1 },
        "search": { "type": "string" },
        "replace": { "type": "string" },
        "file_hash": { "type": "string", "minLength": 1 }
      }
    },
    "file_unified_diff": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "diff", "file_hash"],
      "properties": {
        "type": { "const": "file.unified_diff" },
        "path": { "type": "string", "minLength": 1 },
        "diff": { "type": "string" },
        "file_hash": { "type": "string", "minLength": 1 }
      }
    },
    "file_write_atomic": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "content"],
      "properties": {
        "type": { "const": "file.write_atomic" },
        "path": { "type": "string", "minLength": 1 },
        "content": { "type": "string" },
        "mode": { "type": "integer", "minimum": 0 },
        "conditions": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "must_not_exist": { "type": "boolean" },
            "file_hash": { "type": "string", "minLength": 1 }
          }
        }
      }
    }
  }
}`

const agentStepSchemaJSON = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["type"],
  "properties": {
    "type": { "type": "string", "enum": ["tool_call", "final"] },
    "tool": { "$ref": "#/$defs/tool_call" },
    "final": { "$ref": "#/$defs/final" }
  },
  "allOf": [
    {
      "if": { "properties": { "type": { "const": "tool_call" } } },
      "then": { "required": ["tool"] }
    },
    {
      "if": { "properties": { "type": { "const": "final" } } },
      "then": { "required": ["final"] }
    }
  ],
  "$defs": {
    "tool_call": {
      "type": "object",
      "additionalProperties": false,
      "required": ["name", "input"],
      "properties": {
        "name": {
          "type": "string",
          "enum": ["fs.list", "fs.read", "search.text", "code.symbols", "exec.run"]
        },
        "input": { "type": "object" }
      }
    },
    "final": {
      "type": "object",
      "additionalProperties": false,
      "required": ["patches"],
      "properties": {
        "patches": {
          "type": "array",
          "minItems": 1,
          "items": { "$ref": "#/$defs/patch" }
        }
      }
    },
    "patch": {
      "oneOf": [
        { "$ref": "#/$defs/file_search_replace" },
        { "$ref": "#/$defs/file_unified_diff" },
        { "$ref": "#/$defs/file_write_atomic" }
      ]
    },
    "file_search_replace": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "search", "replace", "file_hash"],
      "properties": {
        "type": { "const": "file.search_replace" },
        "path": { "type": "string", "minLength": 1 },
        "search": { "type": "string" },
        "replace": { "type": "string" },
        "file_hash": { "type": "string", "minLength": 1 }
      }
    },
    "file_unified_diff": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "diff", "file_hash"],
      "properties": {
        "type": { "const": "file.unified_diff" },
        "path": { "type": "string", "minLength": 1 },
        "diff": { "type": "string" },
        "file_hash": { "type": "string", "minLength": 1 }
      }
    },
    "file_write_atomic": {
      "type": "object",
      "additionalProperties": false,
      "required": ["type", "path", "content"],
      "properties": {
        "type": { "const": "file.write_atomic" },
        "path": { "type": "string", "minLength": 1 },
        "content": { "type": "string" },
        "mode": { "type": "integer", "minimum": 0 },
        "conditions": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "must_not_exist": { "type": "boolean" },
            "file_hash": { "type": "string", "minLength": 1 }
          }
        }
      }
    }
  }
}`
