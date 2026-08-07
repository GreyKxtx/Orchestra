package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// openMCPDialog loads mcp.servers from .orchestra.yml and pushes the list modal.
func (a *App) openMCPDialog() {
	servers := a.loadMCPServerViews()
	a.pushDialog(view.NewMCPListDialog(servers, a.cfg.WorkspaceRoot))
}

func (a *App) loadMCPServerViews() []view.MCPServerView {
	if a.cfg.ConfigPath == "" {
		return nil
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil || cfg == nil {
		return nil
	}
	out := make([]view.MCPServerView, 0, len(cfg.MCP.Servers))
	for _, s := range cfg.MCP.Servers {
		out = append(out, view.MCPServerViewFromConfig(s))
	}
	return out
}

func (a *App) refreshMCPListDialog() {
	if d, ok := a.topDialog().(*view.MCPListDialog); ok {
		d.SetServers(a.loadMCPServerViews())
	}
}

// handleMCPDialogResult handles mcp / mcp_preset / mcp_edit dialog results.
func (a *App) handleMCPDialogResult(m view.DialogResultMsg) (tea.Model, tea.Cmd) {
	switch m.Source {
	case "mcp":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "add":
			a.pushDialog(view.NewMCPPresetDialog(a.cfg.WorkspaceRoot))
			return a, nil
		case "edit":
			srv, _ := m.Data.(view.MCPServerView)
			a.pushDialog(view.NewMCPEditDialogFromView(srv))
			return a, nil
		case "delete":
			name, _ := m.Data.(string)
			if err := a.mutateMCPConfig(func(cfg *config.ProjectConfig) {
				cfg.MCP.Servers = filterMCPServers(cfg.MCP.Servers, name)
			}); err != nil {
				a.showToast("mcp: " + err.Error())
				return a, nil
			}
			a.refreshMCPListDialog()
			a.showToast("удалён · " + name)
			return a, a.respawnRPCCmd()
		case "toggle":
			name, _ := m.Data.(string)
			if err := a.mutateMCPConfig(func(cfg *config.ProjectConfig) {
				for i := range cfg.MCP.Servers {
					if cfg.MCP.Servers[i].Name == name {
						cfg.MCP.Servers[i].Disabled = !cfg.MCP.Servers[i].Disabled
						break
					}
				}
			}); err != nil {
				a.showToast("mcp: " + err.Error())
				return a, nil
			}
			a.refreshMCPListDialog()
			a.showToast("toggle · " + name)
			return a, a.respawnRPCCmd()
		case "test":
			name, _ := m.Data.(string)
			a.showToast("MCP test…")
			return a, a.testMCPCmd(name)
		}
	case "mcp_preset":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "select":
			p, _ := m.Data.(view.MCPPreset)
			a.popDialog() // leave list underneath
			a.pushDialog(view.NewMCPEditDialogFromPreset(p))
			return a, nil
		}
	case "mcp_edit":
		switch m.Action {
		case "cancel":
			a.popDialog()
			return a, nil
		case "save":
			r, ok := m.Data.(view.MCPEditDialogResult)
			if !ok {
				a.popDialog()
				return a, nil
			}
			if err := a.saveMCPServer(r); err != nil {
				a.showToast("mcp: " + err.Error())
				return a, nil
			}
			a.popDialog() // back to list
			a.refreshMCPListDialog()
			a.showToast("saved · " + r.Server.Name + " — reloading core…")
			return a, a.respawnRPCCmd()
		}
	}
	return a, nil
}

func filterMCPServers(servers []config.MCPServerConfig, removeName string) []config.MCPServerConfig {
	out := servers[:0:0]
	for _, s := range servers {
		if s.Name == removeName {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (a *App) mutateMCPConfig(fn func(*config.ProjectConfig)) error {
	if a.cfg.ConfigPath == "" {
		return fmt.Errorf("нет .orchestra.yml")
	}
	cfg, err := config.Load(a.cfg.ConfigPath)
	if err != nil {
		return err
	}
	fn(cfg)
	return config.Save(a.cfg.ConfigPath, cfg)
}

func (a *App) saveMCPServer(r view.MCPEditDialogResult) error {
	return a.mutateMCPConfig(func(cfg *config.ProjectConfig) {
		next := r.Server.ToConfig()
		key := r.OriginalName
		if key == "" {
			key = next.Name
		}
		out := make([]config.MCPServerConfig, 0, len(cfg.MCP.Servers)+1)
		replaced := false
		for _, s := range cfg.MCP.Servers {
			if s.Name == key || s.Name == next.Name {
				if !replaced {
					out = append(out, next)
					replaced = true
				}
				continue
			}
			out = append(out, s)
		}
		if !replaced {
			out = append(out, next)
		}
		cfg.MCP.Servers = out
	})
}

type mcpTestMsg struct {
	server string
	out    string
	err    error
}

// testMCPCmd connects to configured MCP servers and lists tools (optionally one).
func (a *App) testMCPCmd(serverName string) tea.Cmd {
	cfgPath := a.cfg.ConfigPath
	return func() tea.Msg {
		if cfgPath == "" {
			return mcpTestMsg{server: serverName, err: fmt.Errorf("нет .orchestra.yml")}
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return mcpTestMsg{server: serverName, err: err}
		}
		mcpCfg := cfg.MCP
		if serverName != "" {
			var one []config.MCPServerConfig
			for _, s := range mcpCfg.Servers {
				if s.Name == serverName {
					one = append(one, s)
					break
				}
			}
			if len(one) == 0 {
				return mcpTestMsg{server: serverName, err: fmt.Errorf("сервер %q не найден", serverName)}
			}
			mcpCfg.Servers = one
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		mgr, errs := mcp.NewManager(ctx, mcpCfg)
		defer mgr.Close()

		var b bytes.Buffer
		defs := mgr.ListToolDefs()
		if len(defs) == 0 && len(errs) == 0 {
			b.WriteString("нет tools (сервер пуст или disabled)")
		}
		byServer := map[string][]string{}
		for _, def := range defs {
			parts := strings.SplitN(def.Function.Name, ":", 3)
			if len(parts) != 3 {
				continue
			}
			byServer[parts[1]] = append(byServer[parts[1]], def.Function.Name)
		}
		for _, srv := range mcpCfg.Servers {
			if srv.Disabled {
				fmt.Fprintf(&b, "▷ %s [disabled]\n", srv.Name)
				continue
			}
			tools := byServer[srv.Name]
			fmt.Fprintf(&b, "▶ %s — %d tool(s)\n", srv.Name, len(tools))
			for _, name := range tools {
				fmt.Fprintf(&b, "  %s\n", name)
			}
		}
		for _, e := range errs {
			fmt.Fprintf(&b, "⚠ %v\n", e)
		}
		return mcpTestMsg{server: serverName, out: strings.TrimSpace(b.String())}
	}
}

func (a *App) handleMCPTestMsg(m mcpTestMsg) {
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "mcp test: "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		a.showToast("MCP ✗")
		return
	}
	text := m.out
	if text == "" {
		text = "ok"
	}
	a.session.AppendSystemNotice(state.SystemKindInfo, "MCP\n"+text)
	a.chat.SetMessages(a.session.Messages)
	a.showToast("MCP OK")
}
