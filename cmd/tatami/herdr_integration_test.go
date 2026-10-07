package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
)

func TestStructuredHostDiscoveryAndRestartWithFakeSSH(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(rootDir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(rootDir, "state"))
	paths, err := config.GetPaths()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(rootDir, "ssh-calls")
	t.Setenv("TATAMI_TEST_LOG", log)
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$TATAMI_TEST_LOG"
printf '%s\n' 'TATAMI-HUB-PROBE 1 inventory' '{"kind":"tatami.hub.inventory","version":1,"host":"fixture","workspaces":[{"name":"API","path":"/srv/api"}],"sessions":[{"name":"agents","running":true}],"hosts":[]}'
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	store := herdrhub.NewStore(paths.HerdrHostsFile)
	p := herdrhub.SavedHost{ID: "box", Label: "Box", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Username: "oles", Port: 2222, Auth: "identity", IdentityFile: "/local/key"}}
	if err := store.SaveProfiles([]herdrhub.SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	endpoints, _, writable, err := loadHerdrHub(paths)
	if err != nil || !writable {
		t.Fatalf("%v %v", err, writable)
	}
	client := herdrhub.NewClient(nil)
	snapshot := client.Query(context.Background(), endpoints[1])
	if snapshot.State != herdrhub.StateOnline || snapshot.RootRevision == "" || len(snapshot.Workspaces) != 1 {
		t.Fatalf("%+v", snapshot)
	}
	first, _ := os.ReadFile(log)
	for _, want := range []string{"BatchMode=yes", "-l\noles\n", "-p\n2222\n", "-i\n/local/key\n", "TATAMI-HUB-PROBE"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("missing %s in fake SSH arguments", want)
		}
	}
	if strings.Count(string(first), "TATAMI-HUB-PROBE 1 inventory") != 1 {
		t.Fatal("discovery reconnected")
	}
	interactive, err := client.QueryInventoryInteractive(context.Background(), endpoints[1], strings.NewReader(""), io.Discard)
	if err != nil || interactive.State != herdrhub.StateOnline {
		t.Fatalf("%+v %v", interactive, err)
	}
	second, _ := os.ReadFile(log)
	if strings.Count(string(second), "TATAMI-HUB-PROBE 1 inventory") != 2 {
		t.Fatal("interactive query reconnected")
	}
	if err := herdrhub.SaveCache(paths.HerdrHubFile, herdrhub.Cache{Snapshots: []herdrhub.Snapshot{snapshot}}); err != nil {
		t.Fatal(err)
	}
	_, cache, _, err := loadHerdrHub(paths)
	if err != nil || len(cache.Snapshots) != 1 || cache.Snapshots[0].State != herdrhub.StateStale {
		t.Fatalf("%+v %v", cache, err)
	}
	p, err = store.Resolve("box")
	if err != nil {
		t.Fatal(err)
	}
	p.Connection.Hostname = "other"
	if err := store.SaveProfiles([]herdrhub.SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	_, cache, _, err = loadHerdrHub(paths)
	if err != nil || len(cache.Snapshots) != 0 {
		t.Fatalf("old cache restored: %+v %v", cache, err)
	}
}
