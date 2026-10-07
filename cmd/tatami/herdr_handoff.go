package main

import (
	"errors"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"github.com/OleksandrBesan/tatami/internal/tui"
)

func resultHubOrigin(result *tui.Result) *sshconn.Origin {
	if result.HubOrigin != nil {
		return result.HubOrigin
	}
	if result.Workspace != nil && result.Workspace.Remote != nil {
		return result.Workspace.Remote.Source
	}
	return nil
}
func resolveHubResult(paths *config.Paths, result *tui.Result) (*tui.Result, error) {
	source := resultHubOrigin(result)
	if source == nil {
		return result, nil
	}
	e, err := herdrhub.ResolveOrigin(herdrhub.NewStore(paths.HerdrHostsFile), source)
	if err != nil {
		return nil, err
	}
	detached := *result
	if result.Workspace != nil {
		ws := *result.Workspace
		if ws.Remote == nil {
			return nil, errors.New("hub workspace remote settings are required")
		}
		remote := *ws.Remote
		c, err := herdrhub.WorkspaceConnection(e, remote.Host, remote.Jump)
		if err != nil {
			return nil, err
		}
		if !sshconn.SafeText(remote.Path) || remote.Path == "" {
			return nil, errors.New("hub workspace path is invalid")
		}
		remote.Connection = &c
		ws.Remote = &remote
		detached.Workspace = &ws
	}
	return &detached, nil
}
