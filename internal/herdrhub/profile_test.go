package herdrhub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/sshconn"
)

func TestSavedHostModesAndProjection(t *testing.T) {
	p := SavedHost{ID: "box", Label: "Box", Group: "Lab", Tags: []string{" Linux ", "Linux"}, Connection: HostConnection{Mode: ModeExplicit, Hostname: "::1", Username: "oles", Port: 2222, Auth: sshconn.Certificate, IdentityFile: "/local/key", CertificateFile: "/local/cert"}}
	if err := ValidateProfile(p); err != nil {
		t.Fatal(err)
	}
	ep, err := p.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	if ep.Connection == nil || ep.Connection.IdentityFile != "/local/key" || ep.Target != "ssh://oles@[::1]:2222" {
		t.Fatalf("%+v", ep)
	}
	ad, ok, err := p.Advertised()
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	b, _ := json.Marshal(ad)
	for _, secret := range []string{"/local", "connection", "auth", "revision", "group", "tags"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s: %s", secret, b)
		}
	}
	p.Connection = HostConnection{Mode: ModeAlias, Alias: "box-alias"}
	if _, ok, err := p.Advertised(); err != nil || ok {
		t.Fatalf("alias leaked: %v %v", ok, err)
	}
	p.Connection.AdvertisedDestination = "oles@box"
	if _, ok, err := p.Advertised(); err != nil || !ok {
		t.Fatalf("alias projection: %v %v", ok, err)
	}
	p.Connection.IdentityFile = "/key"
	if err := ValidateProfile(p); err == nil {
		t.Fatal("alias duplicate settings accepted")
	}
}
func TestProfileValidationAndStableGeneratedIDs(t *testing.T) {
	for _, c := range []HostConnection{
		{Mode: ModeExplicit, Hostname: "host", Port: -1},
		{Mode: ModeExplicit, Hostname: "user@host"},
		{Mode: ModeExplicit, Hostname: "host", Auth: sshconn.SecurityKey},
		{Mode: "other", Hostname: "host"},
		{Mode: ModeAlias, Alias: "a", Username: "u"},
		{Mode: ModeExplicit, Hostname: "host", TatamiExecutable: "evil;cmd"},
	} {
		if err := ValidateProfile(SavedHost{ID: "box", Label: "Box", Connection: c}); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	if got := GenerateHostID("My Laptop", []SavedHost{{ID: "my-laptop"}}); got != "my-laptop-2" {
		t.Fatal(got)
	}
	if got := GenerateHostID("🔑", nil); got != "host" {
		t.Fatal(got)
	}
	for _, target := range []string{"u@box", "ssh://u@box:2222"} {
		p := LegacyProfile(Endpoint{ID: "box", Label: "Box", Target: target})
		if p.Target != target || p.Connection.Mode != ModeLegacy || p.ConnectivityRevision == "" {
			t.Fatalf("%+v", p)
		}
	}
}
