package herdrhub

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationBackupPublicationFailurePreservesSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, strings.Repeat("h", 250))
	source := []byte(`{"hosts":[{"id":"box","label":"Box","target":"box"}]}`)
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	// The source basename fits common POSIX filesystems, but the backup suffix exceeds NAME_MAX.
	if err := store.SaveProfiles(profiles); err == nil {
		t.Fatal("backup publication unexpectedly succeeded")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, source) {
		t.Fatal("publication failure replaced config")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if len(leftovers) != 0 {
		t.Fatal("incomplete backup temporary leaked")
	}
	repaired := filepath.Join(dir, "hosts.json")
	if err := os.Rename(path, repaired); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(repaired).SaveProfiles(profiles); err != nil {
		t.Fatalf("retry after path repair: %v", err)
	}
	data, _ = os.ReadFile(repaired + ".v1.bak")
	if !bytes.Equal(data, source) {
		t.Fatal("retry backup not exact")
	}
}
