package herdrhub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

const (
	InventoryKind          = "tatami.hub.inventory"
	InventoryVersion       = 1
	MaxInventoryWorkspaces = 4096
	MaxInventorySessions   = 512
	MaxInventoryHosts      = 256
	MaxRouteDepth          = 4
)

// WorkspaceSummary is the display and open metadata shared with an
// authenticated Tatami peer. Layout commands and key paths are excluded.
type WorkspaceSummary struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Folder      string   `json:"folder,omitempty"`
	QuickAccess bool     `json:"quick_access,omitempty"`
	Target      string   `json:"target,omitempty"`
	Jump        []string `json:"jump,omitempty"`
}

type SessionSummary struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Default bool   `json:"default,omitempty"`
}

// Inventory is the versioned, read-only Tatami federation contract.
type Inventory struct {
	Kind       string             `json:"kind"`
	Version    int                `json:"version"`
	Host       string             `json:"host"`
	Workspaces []WorkspaceSummary `json:"workspaces"`
	Sessions   []SessionSummary   `json:"sessions"`
	Hosts      []Endpoint         `json:"hosts"`
}

func BuildInventory(host string, workspaces []workspace.Workspace, sessions []shell.HerdrSession, endpoints []Endpoint) (Inventory, error) {
	inventory := Inventory{
		Kind:       InventoryKind,
		Version:    InventoryVersion,
		Host:       host,
		Workspaces: make([]WorkspaceSummary, 0, len(workspaces)),
		Sessions:   make([]SessionSummary, 0, len(sessions)),
		Hosts:      make([]Endpoint, 0, min(len(endpoints), MaxInventoryHosts)),
	}
	if len(workspaces) > MaxInventoryWorkspaces || len(sessions) > MaxInventorySessions || len(endpoints) > MaxInventoryHosts+1 {
		return Inventory{}, errors.New("Tatami inventory exceeds supported limits")
	}
	for _, ws := range workspaces {
		summary := WorkspaceSummary{Name: ws.Name, Path: ws.Path, Folder: ws.Folder, QuickAccess: ws.QuickAccess}
		if ws.IsRemote() {
			summary.Target = ws.Remote.Host
			summary.Jump = append([]string(nil), ws.Remote.Jump...)
			if ws.Remote.Path != "" {
				summary.Path = ws.Remote.Path
			}
		}
		if err := validateWorkspaceSummary(summary); err != nil {
			return Inventory{}, err
		}
		inventory.Workspaces = append(inventory.Workspaces, summary)
	}
	for _, session := range sessions {
		summary := SessionSummary{Name: session.Name, Running: session.Running, Default: session.Default}
		if err := validateRemoteSessionName(summary.Name); err != nil {
			return Inventory{}, err
		}
		inventory.Sessions = append(inventory.Sessions, summary)
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == LocalEndpointID {
			continue
		}
		endpoint.NodeID = ""
		endpoint.Via = nil
		endpoint.Kind = EndpointSSH
		if err := ValidateEndpoint(endpoint); err != nil {
			return Inventory{}, err
		}
		inventory.Hosts = append(inventory.Hosts, endpoint)
	}
	if len(inventory.Hosts) > MaxInventoryHosts {
		return Inventory{}, errors.New("Tatami host inventory exceeds supported limits")
	}
	if err := validateDisplayField("inventory host", host, 128); err != nil {
		return Inventory{}, err
	}
	return inventory, nil
}

func ParseInventory(data []byte) (Inventory, error) {
	var inventory Inventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		return Inventory{}, fmt.Errorf("parse Tatami inventory: %w", err)
	}
	if inventory.Kind != InventoryKind {
		return Inventory{}, fmt.Errorf("unsupported Tatami inventory kind %q", inventory.Kind)
	}
	if inventory.Version != InventoryVersion {
		return Inventory{}, fmt.Errorf("unsupported Tatami inventory version %d", inventory.Version)
	}
	if len(inventory.Workspaces) > MaxInventoryWorkspaces || len(inventory.Sessions) > MaxInventorySessions || len(inventory.Hosts) > MaxInventoryHosts {
		return Inventory{}, errors.New("Tatami inventory exceeds supported limits")
	}
	if err := validateDisplayField("inventory host", inventory.Host, 128); err != nil {
		return Inventory{}, err
	}
	for _, summary := range inventory.Workspaces {
		if err := validateWorkspaceSummary(summary); err != nil {
			return Inventory{}, err
		}
	}
	for _, session := range inventory.Sessions {
		if err := validateRemoteSessionName(session.Name); err != nil {
			return Inventory{}, fmt.Errorf("unsafe inventory session: %w", err)
		}
	}
	seen := make(map[string]bool, len(inventory.Hosts))
	for i := range inventory.Hosts {
		inventory.Hosts[i].Kind = EndpointSSH
		if err := ValidateEndpoint(inventory.Hosts[i]); err != nil {
			return Inventory{}, fmt.Errorf("unsafe inventory host: %w", err)
		}
		if seen[inventory.Hosts[i].ID] {
			return Inventory{}, fmt.Errorf("duplicate inventory host id %q", inventory.Hosts[i].ID)
		}
		seen[inventory.Hosts[i].ID] = true
	}
	return inventory, nil
}

func validateWorkspaceSummary(summary WorkspaceSummary) error {
	if strings.TrimSpace(summary.Name) == "" || strings.TrimSpace(summary.Path) == "" {
		return errors.New("inventory workspace name and path are required")
	}
	if err := validateDisplayField("inventory workspace name", summary.Name, 256); err != nil {
		return err
	}
	if err := validateDisplayField("inventory workspace path", summary.Path, 4096); err != nil {
		return err
	}
	if err := validateDisplayField("inventory workspace folder", summary.Folder, 1024); err != nil {
		return err
	}
	if summary.Target != "" {
		endpoint := Endpoint{ID: "workspace", Label: "Workspace", Target: summary.Target, Via: summary.Jump}
		if err := validateRoutedEndpoint(endpoint); err != nil {
			return fmt.Errorf("unsafe inventory workspace route: %w", err)
		}
	} else if len(summary.Jump) > 0 {
		return errors.New("inventory workspace jump route requires a target")
	}
	return nil
}

// DescendantEndpoint turns a host-local saved endpoint into a local route.
func DescendantEndpoint(parent, child Endpoint) (Endpoint, error) {
	if err := validateRoutedEndpoint(parent); err != nil {
		return Endpoint{}, err
	}
	child.NodeID = ""
	child.Via = nil
	child.Kind = EndpointSSH
	if err := ValidateEndpoint(child); err != nil {
		return Endpoint{}, err
	}
	if len(parent.Via)+2 > MaxRouteDepth {
		return Endpoint{}, fmt.Errorf("remote route exceeds maximum depth %d", MaxRouteDepth)
	}
	for _, target := range append(append([]string(nil), parent.Via...), parent.Target) {
		if target == child.Target {
			return Endpoint{}, errors.New("remote route contains a cycle")
		}
	}
	for _, id := range strings.Split(parent.Key(), "/") {
		if id == child.ID {
			return Endpoint{}, errors.New("remote route contains a cycle")
		}
	}
	child.NodeID = parent.Key() + "/" + child.ID
	child.Via = append(append([]string(nil), parent.Via...), parent.Target)
	return child, nil
}

func validateRoutedEndpoint(endpoint Endpoint) error {
	if err := ValidateEndpoint(endpoint); err != nil {
		return err
	}
	if len(endpoint.Via)+1 > MaxRouteDepth {
		return fmt.Errorf("remote route exceeds maximum depth %d", MaxRouteDepth)
	}
	seen := make(map[string]bool, len(endpoint.Via)+1)
	for _, target := range append(append([]string(nil), endpoint.Via...), endpoint.Target) {
		if err := validateSSHDestination(target); err != nil {
			return err
		}
		if seen[target] {
			return errors.New("remote route contains a cycle")
		}
		seen[target] = true
	}
	return nil
}

// InventoryQueryArgs returns a route-aware SSH command. Authentication is
// always owned by the local OpenSSH process; agent forwarding is never used.
func InventoryQueryArgs(endpoint Endpoint, batch bool) (string, []string, error) {
	if endpoint.ID == LocalEndpointID || endpoint.Kind == EndpointLocal {
		return "tatami", []string{"hub", "inventory", "--json"}, nil
	}
	if err := validateRoutedEndpoint(endpoint); err != nil {
		return "", nil, err
	}
	args := make([]string, 0, 12)
	if batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	if len(endpoint.Via) > 0 {
		args = append(args, "-J", strings.Join(endpoint.Via, ","))
	}
	args = append(args, "--", endpoint.Target, "tatami", "hub", "inventory", "--json")
	return "ssh", args, nil
}
