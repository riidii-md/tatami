package herdrhub

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/sshconn"
)

func TestProfileStoreMigrationBackupAndRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr-hosts.json")
	original, err := os.ReadFile("testdata/hosts-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(b, original) {
		t.Fatal("read rewrote v1")
	}
	revision := profiles[0].ConnectivityRevision
	profiles[0].Label = "Renamed"
	profiles[0].Group = "Lab"
	if err := store.SaveProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(path + ".v1.bak")
	if !bytes.Equal(backup, original) {
		t.Fatalf("backup=%s", backup)
	}
	info, _ := os.Stat(path + ".v1.bak")
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	profiles, err = store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ConnectivityRevision != revision {
		t.Fatal("display edit invalidated connection")
	}
	profiles[0].Connection = HostConnection{Mode: ModeExplicit, Hostname: "box", Username: "u", Auth: sshconn.Identity, IdentityFile: "/local/key"}
	if err := store.SaveProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.Resolve("box")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ConnectivityRevision == revision {
		t.Fatal("connection edit retained revision")
	}
	rev2 := resolved.ConnectivityRevision
	if err := store.Delete("box"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfiles([]SavedHost{resolved}); err != nil {
		t.Fatal(err)
	}
	resolved, err = store.Resolve("box")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ConnectivityRevision == rev2 {
		t.Fatal("delete/readd retained revision")
	}
	backup2, _ := os.ReadFile(path + ".v1.bak")
	if !bytes.Equal(backup2, backup) {
		t.Fatal("backup overwritten")
	}
	data, _ := os.ReadFile(path)
	var legacy struct {
		Hosts []Endpoint `json:"hosts"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil || len(legacy.Hosts) != 1 {
		t.Fatalf("old reader: %v %s", err, data)
	}
}
func TestMigrationConflictingBackupPreservesConfig(t *testing.T) {
	for _, tc := range []string{"partial", "mismatching", "public", "symlink", "matching"} {
		t.Run(tc, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hosts.json")
			original := []byte(`{"hosts":[{"id":"box","label":"Box","target":"box"}]}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			backup := path + ".v1.bak"
			data := original
			mode := os.FileMode(0600)
			switch tc {
			case "partial":
				data = []byte("{")
			case "mismatching":
				data = []byte(`{"hosts":[]}`)
			case "public":
				mode = 0644
			}
			if tc == "symlink" {
				if err := os.Symlink(path, backup); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(backup, data, mode); err != nil {
				t.Fatal(err)
			}
			s := NewStore(path)
			profiles, _ := s.ListProfiles()
			err := s.SaveProfiles(profiles)
			if tc == "matching" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("conflict accepted")
				}
				current, _ := os.ReadFile(path)
				if !bytes.Equal(current, original) {
					t.Fatal("config mutated")
				}
			}
		})
	}
}
func TestProfileStoreRejectsCorruptionAndPreservesStructuredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	s := NewStore(path)
	p := SavedHost{ID: "box", Label: "Box", Connection: HostConnection{Mode: ModeExplicit, Hostname: "box", Auth: sshconn.Identity, IdentityFile: "key"}}
	if err := s.SaveProfiles([]SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	ep, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	ep[1].Label = "New label"
	if err := s.Save(ep); err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve("box")
	if err != nil || !filepath.IsAbs(got.Connection.IdentityFile) {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{`{"version":3,"hosts":[]}`, `{"version":2,"hosts":[{"id":"box"}]}`, "{"} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListProfiles(); err == nil {
			t.Fatal("corruption accepted")
		}
		if err := s.SaveProfiles(nil); err == nil {
			t.Fatal("corruption overwritten")
		}
	}
	b, _ := os.ReadFile(path)
	if !strings.EqualFold(string(b), "{") {
		t.Fatal("changed corruption")
	}
}
