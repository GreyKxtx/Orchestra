package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/xeipuuv/gojsonschema"
)

type Kind string

const (
	KindPlan            Kind = "plan"
	KindExternalPatches Kind = "external_patches"
	KindAgentStep       Kind = "agent_step"
)

type Validator struct {
	plan            *gojsonschema.Schema
	externalPatches *gojsonschema.Schema
	agentStep       *gojsonschema.Schema
}

func NewValidator() (*Validator, error) {
	plan, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(planSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("compile plan schema: %w", err)
	}
	patches, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(externalPatchesSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("compile external patches schema: %w", err)
	}
	step, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(agentStepSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("compile agent step schema: %w", err)
	}
	return &Validator{
		plan:            plan,
		externalPatches: patches,
		agentStep:       step,
	}, nil
}

func (v *Validator) ValidateAndDecode(kind Kind, raw string, out any) *protocol.Error {
	if v == nil {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: validator is nil", nil)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: empty JSON", nil)
	}

	var val any
	if err := json.Unmarshal([]byte(raw), &val); err != nil {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}

	sch := v.schemaFor(kind)
	if sch == nil {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: unknown schema kind", map[string]any{"kind": kind})
	}

	res, err := sch.Validate(gojsonschema.NewGoLoader(val))
	if err != nil {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: schema validation failed: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}
	if !res.Valid() {
		errs := make([]string, 0, len(res.Errors()))
		for _, e := range res.Errors() {
			errs = append(errs, e.String())
			if len(errs) >= 20 {
				break
			}
		}
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: schema validation failed", map[string]any{
			"errors": errs,
		})
	}

	// Decode into the target struct using DisallowUnknownFields (extra safety).
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}
	// Ensure there are no trailing tokens.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}

	return nil
}

func (v *Validator) Validate(kind Kind, raw string) (json.RawMessage, *protocol.Error) {
	raw = strings.TrimSpace(raw)
	var msg json.RawMessage
	if raw == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: empty JSON", nil)
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}

	var val any
	if err := json.Unmarshal(msg, &val); err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}

	sch := v.schemaFor(kind)
	if sch == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: unknown schema kind", map[string]any{"kind": kind})
	}
	res, err := sch.Validate(gojsonschema.NewGoLoader(val))
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: schema validation failed: "+err.Error(), map[string]any{
			"error": err.Error(),
		})
	}
	if !res.Valid() {
		errs := make([]string, 0, len(res.Errors()))
		for _, e := range res.Errors() {
			errs = append(errs, e.String())
			if len(errs) >= 20 {
				break
			}
		}
		return msg, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: schema validation failed", map[string]any{
			"errors": errs,
		})
	}
	// Normalize JSON (trim whitespace etc).
	msg = bytes.TrimSpace(msg)
	return msg, nil
}

func (v *Validator) schemaFor(kind Kind) *gojsonschema.Schema {
	switch kind {
	case KindPlan:
		return v.plan
	case KindExternalPatches:
		return v.externalPatches
	case KindAgentStep:
		return v.agentStep
	default:
		return nil
	}
}
