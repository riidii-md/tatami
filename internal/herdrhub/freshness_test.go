package herdrhub

import (
	"os"
	"path/filepath"
	"testing"
)

func boundEndpoint(t *testing.T, target, revision string) Endpoint {
	t.Helper()
	p := LegacyProfile(Endpoint{ID: "root", Label: "Root", Target: target})
	p.ConnectivityRevision = revision
	e, err := p.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestRouteBoundCacheReconciliation(t *testing.T) {
	root := boundEndpoint(t, "bastion", "root-revision")
	child, err := DescendantEndpoint(root, Endpoint{ID: "child", Label: "Child", Target: "target-a"})
	if err != nil {
		t.Fatal(err)
	}
	parent := StampSnapshot(root, Snapshot{State: StateOnline, Hosts: []Endpoint{{ID: "child", Label: "Child", Target: "target-a"}}})
	old := StampSnapshot(child, Snapshot{State: StateOnline, Workspaces: []WorkspaceSummary{{Name: "A", Path: "/only-a"}}})
	all := []Snapshot{parent, old}
	if got := ReconcileSnapshots([]Endpoint{root}, all, false); len(got) != 2 {
		t.Fatalf("lost valid route %+v", got)
	}
	parent.Hosts[0].Target = "target-b"
	got := ReconcileSnapshots([]Endpoint{root}, []Snapshot{parent, old}, false)
	if len(got) != 1 {
		t.Fatalf("old A revived on B: %+v", got)
	}
	parent.Hosts = nil
	if got := ReconcileSnapshots([]Endpoint{root}, []Snapshot{parent, old}, false); len(got) != 1 {
		t.Fatalf("removed child retained: %+v", got)
	}
	changed := boundEndpoint(t, "bastion", "new-revision")
	if got := ReconcileSnapshots([]Endpoint{changed}, all, false); len(got) != 0 {
		t.Fatal("old root revision accepted")
	}
	if got := ReconcileSnapshots([]Endpoint{root}, []Snapshot{old}, true); len(got) != 0 {
		t.Fatal("orphan restored")
	}
	restored := ReconcileSnapshots([]Endpoint{root}, all, true)
	for _, s := range restored {
		if s.State != StateStale {
			t.Fatalf("restored snapshot online %+v", s)
		}
	}
}
func TestV1CacheDiscardedAndMalformedV2Preserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	old := []byte(`{"version":1,"snapshots":[{"endpoint_id":"old","state":"online"}]}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCache(path)
	if err != nil || len(c.Snapshots) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(old) {
		t.Fatal("load rewrote cache")
	}
	if err := SaveCache(path, Cache{}); err != nil {
		t.Fatal(err)
	}
	malformed := []byte(`{"version":2,"snapshots":[{"endpoint_id":"old","state":"online"}]}`)
	if err := os.WriteFile(path, malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(path); err == nil {
		t.Fatal("unbound v2 accepted")
	}
	b, _ = os.ReadFile(path)
	if string(b) != string(malformed) {
		t.Fatal("malformed v2 changed")
	}
}
func TestStructuredRootHopNeedsAlias(t *testing.T) {
	p := SavedHost{ID: "root", Label: "Root", ConnectivityRevision: "revision", Connection: HostConnection{Mode: ModeExplicit, Hostname: "root", Auth: "identity", IdentityFile: "/key"}}
	root, err := p.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DescendantEndpoint(root, Endpoint{ID: "child", Label: "Child", Target: "child"}); err == nil {
		t.Fatal("root key silently applied to jump")
	}
	p.Connection.JumpAlias = "root-hop"
	root, _ = p.Endpoint()
	child, err := DescendantEndpoint(root, Endpoint{ID: "child", Label: "Child", Target: "child"})
	if err != nil {
		t.Fatal(err)
	}
	if len(child.Via) != 1 || child.Via[0] != "root-hop" || child.Connection != nil {
		t.Fatalf("%+v", child)
	}
}
