package main

import (
	"bytes"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistedDescendantsAndCacheWriteFailureRestartBoundary(t *testing.T) {
	dir := t.TempDir()
	paths := &config.Paths{HerdrHostsFile: filepath.Join(dir, "hosts.json"), HerdrHubFile: filepath.Join(dir, "cache.json")}
	store := herdrhub.NewStore(paths.HerdrHostsFile)
	if err := store.SaveProfiles([]herdrhub.SavedHost{herdrhub.LegacyProfile(herdrhub.Endpoint{ID: "root", Label: "Root", Target: "root"})}); err != nil {
		t.Fatal(err)
	}
	roots, _ := store.List()
	root := roots[1]
	savedChild := herdrhub.Endpoint{ID: "child", Label: "Child", Target: "child-a"}
	child, _ := herdrhub.DescendantEndpoint(root, savedChild)
	parent := herdrhub.StampSnapshot(root, herdrhub.Snapshot{State: herdrhub.StateOnline, LastSuccess: time.Now(), Hosts: []herdrhub.Endpoint{savedChild}})
	descendant := herdrhub.StampSnapshot(child, herdrhub.Snapshot{State: herdrhub.StateOnline, LastSuccess: time.Now(), Workspaces: []herdrhub.WorkspaceSummary{{Name: "child-project", Path: "/srv/child"}}})
	if err := herdrhub.SaveCache(paths.HerdrHubFile, herdrhub.Cache{Snapshots: []herdrhub.Snapshot{parent, descendant}}); err != nil {
		t.Fatal(err)
	}
	_, loaded, _, err := loadHerdrHub(paths)
	if err != nil || len(loaded.Snapshots) != 2 {
		t.Fatalf("descendant restore %+v %v", loaded, err)
	}
	for _, s := range loaded.Snapshots {
		if s.State != herdrhub.StateStale {
			t.Fatalf("restart presented online: %+v", s)
		}
	}
	original, _ := os.ReadFile(paths.HerdrHubFile)
	// Real cache-replacement failure: preserve the old file and replace its target with a directory.
	preserved := filepath.Join(dir, "old-cache.json")
	if err := os.Rename(paths.HerdrHubFile, preserved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.HerdrHubFile, 0700); err != nil {
		t.Fatal(err)
	}
	parent.Hosts = nil
	reconciled := herdrhub.ReconcileSnapshots(roots, []herdrhub.Snapshot{parent, descendant}, false)
	if err := herdrhub.SaveCache(paths.HerdrHubFile, herdrhub.Cache{Snapshots: reconciled}); err == nil {
		t.Fatal("cache replacement fault did not fail")
	}
	bytesAfter, _ := os.ReadFile(preserved)
	if !bytes.Equal(bytesAfter, original) {
		t.Fatal("failed write altered preserved cache")
	}
	if err := os.Remove(paths.HerdrHubFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(preserved, paths.HerdrHubFile); err != nil {
		t.Fatal(err)
	}
	// Approved bounded guarantee: unchanged same-route stale topology can recover after failed persistence.
	_, loaded, _, err = loadHerdrHub(paths)
	if err != nil || len(loaded.Snapshots) != 2 {
		t.Fatalf("bounded stale restore %+v %v", loaded, err)
	}
	for _, s := range loaded.Snapshots {
		if s.State != herdrhub.StateStale {
			t.Fatal("failure restart restored online")
		}
	}
	if err := herdrhub.SaveCache(paths.HerdrHubFile, herdrhub.Cache{Snapshots: reconciled}); err != nil {
		t.Fatal(err)
	}
	_, loaded, _, err = loadHerdrHub(paths)
	if err != nil || len(loaded.Snapshots) != 1 {
		t.Fatalf("persisted removal revived child: %+v %v", loaded, err)
	}
	parent.Hosts = []herdrhub.Endpoint{{ID: "child", Label: "Child", Target: "child-b"}}
	reconciled = herdrhub.ReconcileSnapshots(roots, []herdrhub.Snapshot{parent, descendant}, false)
	if err := herdrhub.SaveCache(paths.HerdrHubFile, herdrhub.Cache{Snapshots: reconciled}); err != nil {
		t.Fatal(err)
	}
	_, loaded, _, err = loadHerdrHub(paths)
	if err != nil || len(loaded.Snapshots) != 1 {
		t.Fatalf("retarget revived old destination: %+v %v", loaded, err)
	}
}
