package core

import (
	"github.com/orchestra/orchestra/internal/telemetry"
	"github.com/orchestra/orchestra/protocol/wire"
)

// teeToTelemetry hands every agent/event notification to the exporter and
// passes it on. A nil notify (no client) becomes one whose only listener is
// the exporter.
func teeToTelemetry(notify func(method string, params any), tel *telemetry.Exporter) func(string, any) {
	if notify == nil {
		notify = func(string, any) {}
	}
	if tel == nil {
		return notify
	}
	return func(method string, params any) {
		if method == wire.NotifyAgentEvent {
			if ev, ok := params.(wire.AgentEvent); ok {
				tel.Event(ev)
			}
		}
		notify(method, params)
	}
}
