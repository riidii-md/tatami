package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestHerdrHostViewHasPersistentFieldsAndExplicitActions(t *testing.T) {
	v := NewHerdrHostView(herdrhub.Endpoint{})
	v.inputs[hostLabel].SetValue("Laptop")
	v.inputs[hostHostname].SetValue("box.local")
	for _, want := range []string{"Alias", "Group", "Tags", "Hostname", "Username", "Port", "SSH", "Authentication", "Save", "Test", "Cancel", "Password prompt (not stored)", "bastion"} {
		if !strings.Contains(v.View(), want) {
			t.Errorf("missing %q: %s", want, v.View())
		}
	}
	for i := 0; i < 40; i++ {
		v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
}
func TestHerdrHostViewModeAndAuthConditionalFields(t *testing.T) {
	v := NewHerdrHostView(herdrhub.Endpoint{ID: "box", Label: "Box", Target: "u@box"})
	if v.mode != herdrhub.ModeLegacy || !strings.Contains(v.View(), "Legacy destination") {
		t.Fatal(v.View())
	}
	v.focus = hostMode
	v.Update(tea.KeyMsg{Type: tea.KeyRight})
	if v.mode != herdrhub.ModeExplicit {
		t.Fatal(v.mode)
	}
	v.inputs[hostHostname].SetValue("box")
	v.inputs[hostIdentity].SetValue("/ignored/key")
	p, err := v.Profile(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Connection.IdentityFile != "" {
		t.Fatal("inactive credential submitted")
	}
	v.focus = hostAuth
	v.Update(tea.KeyMsg{Type: tea.KeyRight})
	if v.auth != "password-prompt" {
		t.Fatal(v.auth)
	}
	if !strings.Contains(v.View(), "Password prompt (not stored)") {
		t.Fatal(v.View())
	}
}
func TestHerdrHostViewShortScreenFollowsFocus(t *testing.T) {
	v := NewHerdrHostView(herdrhub.Endpoint{})
	v.SetSize(42, 10)
	for i := 0; i < 40; i++ {
		visible := v.visibleSlots()
		slot := v.focus
		if slot == hostSave && !strings.Contains(v.View(), "Save") {
			t.Fatal("save inaccessible")
		}
		v.Update(tea.KeyMsg{Type: tea.KeyTab})
		if len(visible) == 0 {
			t.Fatal("no visible slots")
		}
	}
	if len(strings.Split(v.View(), "\n")) > 10 {
		t.Fatalf("form overflows height: %d\n%s", len(strings.Split(v.View(), "\n")), v.View())
	}
}

func TestHerdrHostViewEightRowsKeepsFocusedControlVisible(t *testing.T) {
	v := NewHerdrHostView(herdrhub.Endpoint{})
	v.SetSize(42, 8)
	for i := 0; i < 40; i++ {
		if rows := len(strings.Split(v.View(), "\n")); rows > 8 {
			t.Fatalf("form overflows: %d rows", rows)
		}
		if v.focus == hostSave && !strings.Contains(v.View(), "Save") {
			t.Fatal("Save hidden")
		}
		v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
}
