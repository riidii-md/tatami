package tui

import (
	"fmt"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type HerdrHostView struct {
	inputs     []textinput.Model
	focus      int
	err        error
	mobileMode bool
}

func NewHerdrHostView(endpoint herdrhub.Endpoint) *HerdrHostView {
	values := []string{endpoint.ID, endpoint.Label, endpoint.Target}
	labels := []string{"id", "label", "SSH alias, user@host, or ssh://..."}
	inputs := make([]textinput.Model, 3)
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Placeholder = labels[i]
		inputs[i].SetValue(values[i])
		inputs[i].Width = 32
	}
	inputs[0].Focus()
	return &HerdrHostView{inputs: inputs}
}
func (v *HerdrHostView) SetMobileMode(enabled bool) { v.mobileMode = enabled }
func (v *HerdrHostView) Endpoint() herdrhub.Endpoint {
	return herdrhub.Endpoint{ID: strings.TrimSpace(v.inputs[0].Value()), Label: strings.TrimSpace(v.inputs[1].Value()), Target: strings.TrimSpace(v.inputs[2].Value())}
}
func (v *HerdrHostView) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "tab" || k.String() == "down") {
		v.inputs[v.focus].Blur()
		v.focus = (v.focus + 1) % len(v.inputs)
		v.inputs[v.focus].Focus()
		return nil
	}
	var cmd tea.Cmd
	v.inputs[v.focus], cmd = v.inputs[v.focus].Update(msg)
	return cmd
}
func (v *HerdrHostView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Herdr Host"))
	b.WriteString("\n\n")
	for _, input := range v.inputs {
		b.WriteString(input.View())
		b.WriteString("\n")
	}
	if v.err != nil {
		b.WriteString(errorStyle.Render(v.err.Error()))
		b.WriteString("\n")
	}
	b.WriteString(mutedStyle.Render("Tatami stores no SSH credentials. Passwordless SSH required."))
	if endpoint := v.Endpoint(); herdrhub.ValidateEndpoint(endpoint) == nil {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("First setup: ssh-copy-id " + endpoint.Target))
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Specific key: use an OpenSSH alias with IdentityFile / ssh-agent."))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("[tab]next  [enter]save & test  [esc]cancel"))
	return renderPanel(b.String(), v.mobileMode)
}

// HerdrHostDeleteView confirms removal of one remote endpoint. Removing a host
// only removes Tatami's inventory entry; it does not mutate the remote Herdr.
type HerdrHostDeleteView struct {
	endpoint   herdrhub.Endpoint
	cursor     int
	mobileMode bool
}

func NewHerdrHostDeleteView(endpoint herdrhub.Endpoint) *HerdrHostDeleteView {
	return &HerdrHostDeleteView{endpoint: endpoint}
}

func (v *HerdrHostDeleteView) SetMobileMode(enabled bool) { v.mobileMode = enabled }
func (v *HerdrHostDeleteView) Confirmed() bool            { return v.cursor == 1 }

func (v *HerdrHostDeleteView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		v.cursor = 1
	case "k", "up":
		v.cursor = 0
	default:
		if v.mobileMode {
			if index, ok := numberKeyIndex(key.String(), 2); ok {
				v.cursor = index
			}
		}
	}
	return nil
}

func (v *HerdrHostDeleteView) View() string {
	labels := []string{"cancel", "remove host"}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Remove Herdr Host"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("Remove %q from Tatami?", v.endpoint.Label)))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("The remote Herdr and its sessions are not changed."))
	b.WriteString("\n\n")
	for i, label := range labels {
		cursor := choicePrefix(v.mobileMode, i, i == v.cursor)
		style := normalStyle
		if i == v.cursor {
			style = selectedStyle
		}
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	help := "\n[enter]select  [esc]back"
	if v.mobileMode {
		help = "\n[↑↓/1-9]select  [enter]confirm  [b]back"
	}
	b.WriteString(helpStyle.Render(help))
	return renderPanel(b.String(), v.mobileMode)
}
