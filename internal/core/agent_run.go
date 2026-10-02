package core

import (
	"context"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/protocol/wire"
)

// continueInSwitchedMode runs the rest of a turn in the mode the user switched
// it to (agent.ContinueInSwitchedMode). The client hears each switch as a
// mode_route whose from is the mode the turn left, and the launch records the
// mode the turn ended in.
func continueInSwitchedMode(
	ctx context.Context,
	launch *agentLaunch,
	notify func(method string, params any),
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	history []llm.Message,
	res *agent.Result,
) ([]llm.Message, *agent.Result, error) {
	onSwitch := func(from, to agent.Mode) {
		launch.EffectiveMode = string(to)
		if notify == nil {
			return
		}
		notify(wire.NotifyAgentEvent, launch.EventEnvelope.stamp(wire.AgentEvent{
			Type: wire.EventModeRoute,
			Data: wire.ModeRoute{From: string(from), To: string(to), Reason: "the user approved the switch"},
		}, nil))
	}
	return agent.ContinueInSwitchedMode(ctx, llmClient, validator, toolRunner, launch.Opts, history, res, onSwitch)
}
