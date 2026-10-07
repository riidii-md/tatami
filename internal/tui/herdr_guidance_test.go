package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"strings"
	"testing"
)

func TestAuthenticationGuidanceUsesConfiguredAuth(t *testing.T) {
	for _, auth := range []sshconn.AuthMethod{sshconn.Certificate, sshconn.PasswordPrompt} {
		p := herdrhub.SavedHost{ID: "box", Label: "Box", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Auth: auth}}
		if auth == sshconn.Certificate {
			p.Connection.IdentityFile = "/local/key"
			p.Connection.CertificateFile = "/local/cert"
		}
		e, err := p.Endpoint()
		if err != nil {
			t.Fatal(err)
		}
		text := hubAuthenticationGuidance(&e, herdrhub.Snapshot{State: herdrhub.StateAuthenticationNeeded})
		wants := []string{"BatchMode=yes"}
		if auth == sshconn.Certificate {
			wants = append(wants, "/local/key", "CertificateFile=", "/local/cert", "IdentitiesOnly=yes")
		} else {
			wants = append(wants, "PubkeyAuthentication=no", "PreferredAuthentications=keyboard-interactive,password", "change Authentication")
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("auth %s missing %q: %s", auth, want, text)
			}
		}
	}
}

func TestNestedWorkspaceShowsMissingJumpAliasGuidance(t *testing.T) {
	p := herdrhub.SavedHost{ID: "box", Label: "Box", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Auth: sshconn.Identity, IdentityFile: "/local/key"}}
	e, err := p.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	l := NewListView(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}))
	l.SetHerdrHubSnapshots([]herdrhub.Endpoint{e}, []herdrhub.Snapshot{herdrhub.StampSnapshot(e, herdrhub.Snapshot{State: herdrhub.StateOnline, Workspaces: []herdrhub.WorkspaceSummary{{Name: "nested", Path: "/srv/nested", Target: "child"}}})})
	for i, item := range l.items {
		if item.Type == "herdr_endpoint" && item.Endpoint.ID == "box" {
			l.cursor = i
		}
	}
	if text := l.herdrEndpointGuidanceView(); !strings.Contains(text, "alias") {
		t.Fatalf("no recoverable hop guidance: %q", text)
	}
}
