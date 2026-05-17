package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeSkillRunner struct {
	lastName string
	lastTask string
	result   string
	err      error
}

func (f *fakeSkillRunner) InvokeSkill(_ context.Context, name, task string) (string, error) {
	f.lastName = name
	f.lastTask = task
	return f.result, f.err
}

func TestHandleSkillInvoke_HappyPath(t *testing.T) {
	fake := &fakeSkillRunner{result: "skill produced X"}
	a := &Agent{opts: Options{
		Skills:      []SkillSpec{{Name: "refactor", Description: "D"}},
		SkillRunner: fake,
	}}
	out, err := a.handleSkillInvoke(context.Background(),
		json.RawMessage(`{"skill":"refactor","task":"do something"}`))
	if err != nil {
		t.Fatalf("handleSkillInvoke: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["skill"] != "refactor" || resp["status"] != "done" || resp["result"] != "skill produced X" {
		t.Errorf("unexpected response: %v", resp)
	}
	if fake.lastName != "refactor" || fake.lastTask != "do something" {
		t.Errorf("fake called with name=%q task=%q", fake.lastName, fake.lastTask)
	}
}

func TestHandleSkillInvoke_UnknownSkill(t *testing.T) {
	a := &Agent{opts: Options{
		Skills:      []SkillSpec{{Name: "refactor"}},
		SkillRunner: &fakeSkillRunner{},
	}}
	_, err := a.handleSkillInvoke(context.Background(),
		json.RawMessage(`{"skill":"nope","task":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown skill") {
		t.Fatalf("expected unknown-skill error, got %v", err)
	}
}

func TestHandleSkillInvoke_MissingFields(t *testing.T) {
	a := &Agent{opts: Options{
		Skills:      []SkillSpec{{Name: "x"}},
		SkillRunner: &fakeSkillRunner{},
	}}
	cases := []struct {
		input string
		want  string
	}{
		{`{"task":"only-task"}`, "skill is required"},
		{`{"skill":"x"}`, "task is required"},
		{`{"skill":"x","task":"   "}`, "task is required"},
		{`not json`, "invalid input"},
	}
	for _, c := range cases {
		_, err := a.handleSkillInvoke(context.Background(), json.RawMessage(c.input))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("input %q: want %q, got %v", c.input, c.want, err)
		}
	}
}

func TestHandleSkillInvoke_RunnerErrorPropagates(t *testing.T) {
	fake := &fakeSkillRunner{err: errors.New("child blew up")}
	a := &Agent{opts: Options{
		Skills:      []SkillSpec{{Name: "x"}},
		SkillRunner: fake,
	}}
	_, err := a.handleSkillInvoke(context.Background(),
		json.RawMessage(`{"skill":"x","task":"t"}`))
	if err == nil || !strings.Contains(err.Error(), "child blew up") {
		t.Fatalf("expected wrapped runner error, got %v", err)
	}
}

func TestSkillsAdvertisement(t *testing.T) {
	a := &Agent{opts: Options{
		Skills: []SkillSpec{
			{Name: "refactor", Description: "Refactor Go code."},
			{Name: "review", Description: "Review changes."},
		},
		SkillRunner: &fakeSkillRunner{},
	}}
	out := a.skillsAdvertisement()
	for _, want := range []string{"<available_skills>", "skill_invoke", "refactor", "Refactor Go code.", "review", "Review changes."} {
		if !strings.Contains(out, want) {
			t.Errorf("advertisement missing %q:\n%s", want, out)
		}
	}
}

func TestSkillsAdvertisement_EmptyWhenNoRunner(t *testing.T) {
	a := &Agent{opts: Options{Skills: []SkillSpec{{Name: "x"}}}}
	if got := a.skillsAdvertisement(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
