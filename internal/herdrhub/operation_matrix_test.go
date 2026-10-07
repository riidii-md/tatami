package herdrhub

import (
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEveryStructuredOperationUsesFinalDestinationSettings(t *testing.T) {
	p := SavedHost{ID: "box", Label: "Box", ConnectivityRevision: "revision", Connection: HostConnection{Mode: ModeExplicit, Hostname: "box", Username: "oles", Port: 2222, Auth: sshconn.Certificate, IdentityFile: "/local/key with space", CertificateFile: "/local/cert with space", Jump: []string{"relay"}, HerdrExecutable: "/opt/bin/herdr"}}
	e, err := p.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		build      func() (string, []string, error)
		batch, tty bool
	}{
		{"inventory", func() (string, []string, error) { return InventoryQueryArgs(e, true) }, true, false},
		{"interactive", func() (string, []string, error) { return InventoryQueryArgs(e, false) }, false, false},
		{"sessions", func() (string, []string, error) { return QueryArgs(e) }, true, false},
		{"interactive-sessions", func() (string, []string, error) { return InteractiveQueryArgs(e) }, false, false},
		{"agents", func() (string, []string, error) { return AgentArgs(e, "agents") }, true, false},
		{"attach", func() (string, []string, error) { return AttachArgs(e, "agents") }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, args, err := tc.build()
			if err != nil || name != "ssh" {
				t.Fatalf("%s %v", name, err)
			}
			has := func(value string) bool {
				for _, a := range args {
					if a == value {
						return true
					}
				}
				return false
			}
			for _, value := range []string{"-l", "oles", "-p", "2222", "-i", "/local/key with space", "IdentitiesOnly=yes", "-J", "relay"} {
				if !has(value) {
					t.Fatalf("lost %s in %q", value, args)
				}
			}
			if has("BatchMode=yes") != tc.batch || has("-t") != tc.tty || has("-A") {
				t.Fatalf("operation flags %q", args)
			}
			if !strings.Contains(strings.Join(args, " "), "CertificateFile=") {
				t.Fatal("certificate lost")
			}
		})
	}
}
func TestFrozenV2AndProfileNormalization(t *testing.T) {
	data, err := os.ReadFile("testdata/hosts-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	profiles, version, err := decodeProfiles(data)
	if err != nil || version != 2 || len(profiles) != 1 {
		t.Fatalf("%v %d", err, version)
	}
	p := profiles[0]
	p.Tags = []string{" Linux ", "Linux", "", "macOS"}
	p, err = NormalizeProfile(p)
	if err != nil || !reflect.DeepEqual(p.Tags, []string{"Linux", "macOS"}) {
		t.Fatalf("%+v %v", p, err)
	}
	e, _ := p.Endpoint()
	inventory, err := BuildInventory("owner", nil, nil, []Endpoint{e})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Hosts) != 1 || inventory.Hosts[0].Target != "ssh://u@box:2222" {
		t.Fatalf("%+v", inventory)
	}
}
