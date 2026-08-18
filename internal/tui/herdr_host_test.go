package tui

import (
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
)

func TestHerdrHostViewExplainsSSHAuthentication(t *testing.T) {
	view := NewHerdrHostView(herdrhub.Endpoint{})
	got := view.View()
	for _, want := range []string{"OpenSSH", "ssh-agent", "IdentityFile"} {
		if !strings.Contains(got, want) {
			t.Errorf("host form missing %q authentication guidance:\n%s", want, got)
		}
	}
}
