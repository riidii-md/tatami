package tui

import (
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
)

func TestHerdrHostViewExplainsSSHAuthentication(t *testing.T) {
	view := NewHerdrHostView(herdrhub.Endpoint{ID: "macmini", Label: "Mac Mini", Target: "oles@bmo.local"})
	got := view.View()
	for _, want := range []string{
		"Tatami stores no SSH credentials",
		"OpenSSH asks for password or key passphrase",
		"Background refresh needs non-interactive SSH",
		"ssh-add ~/.ssh/<private-key>",
		"ssh-copy-id oles@bmo.local",
		"ssh -o BatchMode=yes oles@bmo.local true",
		"IdentityFile",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("host form missing %q authentication guidance:\n%s", want, got)
		}
	}
}

func TestHerdrHostViewDoesNotBuildSetupCommandFromUnsafeTarget(t *testing.T) {
	view := NewHerdrHostView(herdrhub.Endpoint{ID: "macmini", Label: "Mac Mini"})
	view.inputs[2].SetValue("host;touch-pwned")
	got := view.View()
	if strings.Contains(got, "ssh-copy-id") {
		t.Fatalf("host form rendered setup command for unsafe destination:\n%s", got)
	}
}
