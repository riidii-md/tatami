package tui

import "github.com/OleksandrBesan/tatami/internal/workspace"

func safeResultWorkspace(ws *workspace.Workspace) *workspace.Workspace {
	if ws == nil || ws.Remote == nil || ws.Remote.Source == nil {
		return ws
	}
	detached := *ws
	remote := *ws.Remote
	remote.Connection = nil
	source := *ws.Remote.Source
	source.Via = append([]string(nil), source.Via...)
	remote.Source = &source
	detached.Remote = &remote
	return &detached
}
