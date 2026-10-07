//go:build linux || darwin

package herdrhub

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMigrationWriteFailureRetries(t *testing.T) {
	mode := os.Getenv("TATAMI_MIGRATION_WRITE_CONTROL")
	if mode == "" {
		for _, scenario := range []string{"backup", "config"} {
			t.Run(scenario, func(t *testing.T) {
				command := exec.Command(os.Args[0], "-test.run=^TestMigrationWriteFailureRetries$")
				command.Env = append(os.Environ(), "TATAMI_MIGRATION_WRITE_CONTROL="+scenario)
				if out, err := command.CombinedOutput(); err != nil {
					t.Fatalf("%v: %s", err, out)
				}
			})
		}
		return
	}
	path := filepath.Join(t.TempDir(), "hosts.json")
	original := []byte(`{"hosts":[{"id":"box","label":"Box","target":"box"}]}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	profiles, err := store.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	constrained := limit
	constrained.Cur = 1
	if mode == "config" {
		constrained.Cur = uint64(len(original))
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &constrained); err != nil {
		t.Fatal(err)
	}
	failed := store.SaveProfiles(profiles)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	if failed == nil {
		t.Fatal("write fault did not fail migration")
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, original) {
		t.Fatal("failed migration changed source")
	}
	backup, backupErr := os.ReadFile(path + ".v1.bak")
	if mode == "backup" {
		if !os.IsNotExist(backupErr) {
			t.Fatal("partial backup was published")
		}
	} else if backupErr != nil || !bytes.Equal(backup, original) {
		t.Fatal("complete backup lost after config-write failure")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tmp-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files leaked: %v", leftovers)
	}
	if err := store.SaveProfiles(profiles); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	backup, err = os.ReadFile(path + ".v1.bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("retry backup incorrect")
	}
	info, _ := os.Stat(path + ".v1.bak")
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup not private")
	}
	if _, err := store.ListProfiles(); err != nil {
		t.Fatal(err)
	}
}
