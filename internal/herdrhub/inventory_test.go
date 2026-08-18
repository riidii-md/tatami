package herdrhub

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestBuildInventoryExportsFullNavigationWithoutSecretsOrCommands(t *testing.T) {
	workspaces := []workspace.Workspace{
		{
			Name:        "API",
			Path:        "/srv/api",
			Folder:      "work/backend",
			QuickAccess: true,
			Layout: workspace.Layout{
				Type:    workspace.LayoutZellij,
				MainCmd: "secret-command --token value",
				Panes:   []workspace.Pane{{Command: "another-secret"}},
			},
		},
		{
			Name: "Remote DB",
			Remote: &workspace.Remote{
				Host: "db.internal",
				Path: "/srv/db",
				Key:  "/home/user/.ssh/private-key",
				Jump: []string{"db-bastion"},
			},
		},
	}
	sessions := []shell.HerdrSession{{Name: "default", Running: true, Default: true}, {Name: "agents"}}
	hosts := []Endpoint{LocalEndpoint(), {ID: "macmini", Label: "Mac Mini", Target: "macmini.internal"}}

	inventory, err := BuildInventory("bastion", workspaces, sessions, hosts)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Kind != InventoryKind || inventory.Version != InventoryVersion || inventory.Host != "bastion" {
		t.Fatalf("inventory identity = %#v", inventory)
	}
	wantWorkspaces := []WorkspaceSummary{
		{Name: "API", Path: "/srv/api", Folder: "work/backend", QuickAccess: true},
		{Name: "Remote DB", Path: "/srv/db", Target: "db.internal", Jump: []string{"db-bastion"}},
	}
	if !reflect.DeepEqual(inventory.Workspaces, wantWorkspaces) {
		t.Fatalf("workspaces = %#v; want %#v", inventory.Workspaces, wantWorkspaces)
	}
	if !reflect.DeepEqual(inventory.Sessions, []SessionSummary{{Name: "default", Running: true, Default: true}, {Name: "agents"}}) {
		t.Fatalf("sessions = %#v", inventory.Sessions)
	}
	wantHosts := []Endpoint{{ID: "macmini", Label: "Mac Mini", Kind: EndpointSSH, Target: "macmini.internal"}}
	if !reflect.DeepEqual(inventory.Hosts, wantHosts) {
		t.Fatalf("hosts = %#v", inventory.Hosts)
	}
	b, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-command", "another-secret", "private-key"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("inventory leaked %q: %s", forbidden, b)
		}
	}
}

func TestParseInventoryRejectsUnsupportedUnsafeAndOversizedData(t *testing.T) {
	valid := `{"kind":"tatami.hub.inventory","version":1,"host":"bastion","workspaces":[{"name":"API","path":"/srv/api","folder":"work"}],"sessions":[{"name":"agents","running":true}],"hosts":[{"id":"macmini","label":"Mac Mini","target":"macmini"}]}`
	inventory, err := ParseInventory([]byte(valid))
	if err != nil || len(inventory.Workspaces) != 1 || len(inventory.Sessions) != 1 || len(inventory.Hosts) != 1 {
		t.Fatalf("valid inventory = %#v err=%v", inventory, err)
	}
	for name, payload := range map[string]string{
		"version":   `{"kind":"tatami.hub.inventory","version":2,"host":"bastion"}`,
		"kind":      `{"kind":"other","version":1,"host":"bastion"}`,
		"workspace": `{"kind":"tatami.hub.inventory","version":1,"host":"bastion","workspaces":[{"name":"bad\u001b[2J","path":"/srv"}]}`,
		"session":   `{"kind":"tatami.hub.inventory","version":1,"host":"bastion","sessions":[{"name":"bad;touch"}]}`,
		"host":      `{"kind":"tatami.hub.inventory","version":1,"host":"bastion","hosts":[{"id":"bad","label":"Bad","target":"-oProxyCommand=x"}]}`,
		"jump":      `{"kind":"tatami.hub.inventory","version":1,"host":"bastion","workspaces":[{"name":"DB","path":"/srv/db","target":"db","jump":["-oProxyCommand=x"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseInventory([]byte(payload)); err == nil {
				t.Fatalf("accepted %s", payload)
			}
		})
	}
	tooMany := Inventory{Kind: InventoryKind, Version: InventoryVersion, Host: "bastion", Workspaces: make([]WorkspaceSummary, MaxInventoryWorkspaces+1)}
	b, _ := json.Marshal(tooMany)
	if _, err := ParseInventory(b); err == nil {
		t.Fatal("accepted oversized workspace inventory")
	}
}

func TestDescendantEndpointBuildsValidatedBoundedRoute(t *testing.T) {
	bastion := Endpoint{ID: "bastion", Label: "Bastion", Target: "user@bastion"}
	macmini, err := DescendantEndpoint(bastion, Endpoint{ID: "macmini", Label: "Mac Mini", Target: "macmini.internal"})
	if err != nil {
		t.Fatal(err)
	}
	if macmini.Key() != "bastion/macmini" || !reflect.DeepEqual(macmini.Via, []string{"user@bastion"}) {
		t.Fatalf("descendant = %#v key=%q", macmini, macmini.Key())
	}
	next, err := DescendantEndpoint(macmini, Endpoint{ID: "gpu", Label: "GPU", Target: "gpu.internal"})
	if err != nil || next.Key() != "bastion/macmini/gpu" || !reflect.DeepEqual(next.Via, []string{"user@bastion", "macmini.internal"}) {
		t.Fatalf("next = %#v key=%q err=%v", next, next.Key(), err)
	}
	if _, err := DescendantEndpoint(macmini, Endpoint{ID: "bastion", Label: "Cycle", Target: "user@bastion"}); err == nil {
		t.Fatal("accepted route cycle")
	}
	deep := bastion
	for i := 0; i < MaxRouteDepth-1; i++ {
		deep, err = DescendantEndpoint(deep, Endpoint{ID: "h" + string(rune('a'+i)), Label: "Hop", Target: "h" + string(rune('a'+i))})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DescendantEndpoint(deep, Endpoint{ID: "too-deep", Label: "Too Deep", Target: "too-deep"}); err == nil {
		t.Fatal("accepted route beyond maximum depth")
	}
}

func TestRouteAwareSSHArgsNeverUseAgentForwarding(t *testing.T) {
	endpoint := Endpoint{ID: "macmini", NodeID: "bastion/macmini", Label: "Mac Mini", Target: "macmini.internal", Via: []string{"user@bastion"}}
	name, args, err := InventoryQueryArgs(endpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-o", "BatchMode=yes", "-J", "user@bastion", "--", "macmini.internal", "tatami", "hub", "inventory", "--json"}
	if name != "ssh" || !reflect.DeepEqual(args, want) {
		t.Fatalf("inventory query = %s %#v; want ssh %#v", name, args, want)
	}
	for _, arg := range args {
		if arg == "-A" || strings.Contains(arg, "ForwardAgent") {
			t.Fatalf("route enabled agent forwarding: %#v", args)
		}
	}
}
