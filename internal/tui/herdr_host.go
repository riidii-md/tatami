package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	hostLabel = iota
	hostGroup
	hostTags
	hostMode
	hostHostname
	hostUsername
	hostPort
	hostJump
	hostAuth
	hostIdentity
	hostCertificate
	hostAdvertised
	hostJumpAlias
	hostTatami
	hostHerdr
	hostLegacyTarget
	hostSave
	hostTest
	hostCancel
)

type HerdrHostView struct {
	inputs        []textinput.Model
	focus         int
	err           error
	notice        string
	mobileMode    bool
	original      herdrhub.SavedHost
	mode          herdrhub.ConnectionMode
	auth          sshconn.AuthMethod
	width, height int
	testSnapshot  *herdrhub.Snapshot
	testedProfile *herdrhub.SavedHost
}

func NewHerdrHostView(endpoint herdrhub.Endpoint) *HerdrHostView {
	p := herdrhub.LegacyProfile(endpoint)
	if endpoint.Profile != nil {
		p = *endpoint.Profile
	}
	if endpoint.ID == "" {
		p = herdrhub.SavedHost{Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit}}
	}
	c := p.Connection
	v := &HerdrHostView{original: p, mode: c.Mode, auth: c.Auth, inputs: make([]textinput.Model, hostSave)}
	if v.auth == "" {
		v.auth = sshconn.ConfigAgent
	}
	host := c.Hostname
	if c.Mode == herdrhub.ModeAlias {
		host = c.Alias
	}
	port := ""
	if c.Port > 0 {
		port = strconv.Itoa(c.Port)
	}
	values := []string{p.Label, p.Group, strings.Join(p.Tags, ", "), string(c.Mode), host, c.Username, port, strings.Join(c.Jump, ","), string(v.auth), c.IdentityFile, c.CertificateFile, c.AdvertisedDestination, c.JumpAlias, c.TatamiExecutable, c.HerdrExecutable, p.Target}
	for i := range v.inputs {
		v.inputs[i] = textinput.New()
		v.inputs[i].Width = 32
		v.inputs[i].CharLimit = 4096
		v.inputs[i].SetValue(values[i])
	}
	v.inputs[0].Focus()
	return v
}
func (v *HerdrHostView) SetMobileMode(enabled bool) { v.mobileMode = enabled }
func (v *HerdrHostView) SetSize(width, height int) {
	v.width, v.height = width, height
	for i := range v.inputs {
		v.inputs[i].Width = max(8, width-28)
	}
}
func (v *HerdrHostView) visibleSlots() []int {
	slots := []int{hostLabel, hostGroup, hostTags, hostMode}
	switch v.mode {
	case herdrhub.ModeLegacy:
		slots = append(slots, hostLegacyTarget)
	case herdrhub.ModeAlias:
		slots = append(slots, hostHostname)
	default:
		slots = append(slots, hostHostname, hostUsername, hostPort, hostJump, hostAuth)
		if v.auth == sshconn.Identity || v.auth == sshconn.Certificate || v.auth == sshconn.SecurityKey {
			slots = append(slots, hostIdentity)
		}
		if v.auth == sshconn.Certificate {
			slots = append(slots, hostCertificate)
		}
	}
	slots = append(slots, hostAdvertised, hostJumpAlias, hostTatami, hostHerdr, hostSave, hostTest, hostCancel)
	return slots
}
func (v *HerdrHostView) move(delta int) {
	slots := v.visibleSlots()
	index := slices.Index(slots, v.focus)
	if index < 0 {
		index = 0
	}
	if v.focus < hostSave {
		v.inputs[v.focus].Blur()
	}
	v.focus = slots[(index+delta+len(slots))%len(slots)]
	if v.focus < hostSave && v.focus != hostMode && v.focus != hostAuth {
		v.inputs[v.focus].Focus()
	}
}
func (v *HerdrHostView) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "tab", "enter":
			v.move(1)
			return nil
		case "shift+tab":
			v.move(-1)
			return nil
		case "right", "left", "up", "down":
			delta := 1
			if key.String() == "left" || key.String() == "up" {
				delta = -1
			}
			if v.focus == hostMode {
				modes := []herdrhub.ConnectionMode{herdrhub.ModeExplicit, herdrhub.ModeAlias}
				if v.original.Connection.Mode == herdrhub.ModeLegacy {
					modes = append([]herdrhub.ConnectionMode{herdrhub.ModeLegacy}, modes...)
				}
				index := slices.Index(modes, v.mode)
				v.mode = modes[(index+delta+len(modes))%len(modes)]
				v.err = nil
				return nil
			}
			if v.focus == hostAuth {
				methods := []sshconn.AuthMethod{sshconn.ConfigAgent, sshconn.PasswordPrompt, sshconn.Identity, sshconn.Certificate, sshconn.SecurityKey}
				index := slices.Index(methods, v.auth)
				v.auth = methods[(index+delta+len(methods))%len(methods)]
				v.err = nil
				return nil
			}
		}
	}
	if v.focus >= hostSave || v.focus == hostMode || v.focus == hostAuth {
		return nil
	}
	var cmd tea.Cmd
	v.inputs[v.focus], cmd = v.inputs[v.focus].Update(msg)
	return cmd
}
func (v *HerdrHostView) Profile(existing []herdrhub.SavedHost) (herdrhub.SavedHost, error) {
	value := func(i int) string { return strings.TrimSpace(v.inputs[i].Value()) }
	p := v.original
	p.Label = value(hostLabel)
	p.Group = value(hostGroup)
	p.Tags = strings.Split(value(hostTags), ",")
	if p.ID == "" {
		p.ID = herdrhub.GenerateHostID(p.Label, existing)
	}
	c := herdrhub.HostConnection{Mode: v.mode, Auth: sshconn.ConfigAgent, AdvertisedDestination: value(hostAdvertised), JumpAlias: value(hostJumpAlias), TatamiExecutable: value(hostTatami), HerdrExecutable: value(hostHerdr)}
	switch v.mode {
	case herdrhub.ModeLegacy:
		c.Auth = v.original.Connection.Auth
		p.Target = value(hostLegacyTarget)
	case herdrhub.ModeAlias:
		c.Alias = value(hostHostname)
	default:
		c.Hostname = value(hostHostname)
		c.Username = value(hostUsername)
		c.Auth = v.auth
		if port := value(hostPort); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return p, fmt.Errorf("Port must be 1–65535")
			}
			c.Port = n
		}
		if jump := value(hostJump); jump != "" {
			for _, hop := range strings.Split(jump, ",") {
				c.Jump = append(c.Jump, strings.TrimSpace(hop))
			}
		}
		if v.auth == sshconn.Identity || v.auth == sshconn.Certificate || v.auth == sshconn.SecurityKey {
			c.IdentityFile = value(hostIdentity)
		}
		if v.auth == sshconn.Certificate {
			c.CertificateFile = value(hostCertificate)
		}
	}
	p.Connection = c
	return herdrhub.NormalizeProfile(p)
}
func (v *HerdrHostView) Endpoint() herdrhub.Endpoint {
	p, err := v.Profile(nil)
	if err != nil {
		return herdrhub.Endpoint{}
	}
	e, _ := p.Endpoint()
	return e
}
func authLabel(method sshconn.AuthMethod) string {
	switch method {
	case sshconn.PasswordPrompt:
		return "Password prompt (not stored)"
	case sshconn.Identity:
		return "SSH identity file"
	case sshconn.Certificate:
		return "SSH certificate + identity"
	case sshconn.SecurityKey:
		return "Security key / FIDO2 identity"
	default:
		return "OpenSSH config / agent"
	}
}
func (v *HerdrHostView) View() string {
	compact := v.mobileMode || (v.height > 0 && v.height < 10)
	labels := []string{"Alias", "Group", "Tags (comma-separated)", "Mode", "Hostname", "Username", "Port", "ProxyJump", "Authentication", "Identity file", "Certificate file", "Advertised destination", "Jump alias", "Tatami executable", "Herdr executable", "Legacy destination", "Save", "Test", "Cancel"}
	if v.mode == herdrhub.ModeAlias {
		labels[hostHostname] = "SSH config alias"
	}
	lines := []string{"Host"}
	focusLine := 0
	for _, slot := range v.visibleSlots() {
		if slot == hostMode {
			lines = append(lines, "Connection · SSH (Mosh is staged)")
		}
		if slot == hostAuth {
			lines = append(lines, "Credentials")
		}
		if slot == hostAdvertised {
			lines = append(lines, "Advanced")
		}
		line := labels[slot]
		if slot < hostSave {
			value := v.inputs[slot].View()
			if slot == hostMode {
				value = string(v.mode) + " [←/→]"
			}
			if slot == hostAuth {
				value = authLabel(v.auth) + " [←/→]"
			}
			line += ": " + value
		} else if slot == v.focus {
			line = "> " + line
		}
		if slot == v.focus {
			focusLine = len(lines)
			line = selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "OpenSSH asks for password, passphrase or security-key PIN/touch.", "Password prompt (not stored). Background refresh needs non-interactive SSH.", "On a bastion, SSH authenticates from the bastion; use keys/config there.", "No password or key contents stored. Encrypted key: ssh-add ~/.ssh/<private-key>")
	status := ""
	if v.err != nil {
		status = v.err.Error()
	} else if v.notice != "" {
		status = v.notice
	}
	body := strings.Join(lines, "\n")
	if v.height > 0 {
		overhead := 8
		if compact {
			overhead = 4
		}
		if status != "" {
			overhead++
		}
		vp := viewport.New(max(12, v.width-6), max(1, v.height-overhead))
		vp.SetContent(body)
		if focusLine >= vp.Height {
			vp.SetYOffset(focusLine - vp.Height + 1)
		}
		body = vp.View()
	}
	help := "[tab/shift-tab]focus [←→]choice [enter]next/action [esc]cancel"
	if v.width > 0 && v.width < 80 {
		help = "[tab]focus [enter]act [esc]back"
	}
	if v.width > 0 && v.width < 38 {
		help = "[tab]focus [↵]act"
	}
	if status != "" {
		if v.width > 0 {
			status = ansi.Truncate(status, max(8, v.width-6), "…")
		}
		body = status + "\n" + body
	}
	return renderPanel(titleStyle.Render("SSH Host")+"\n"+body+"\n"+helpStyle.Render(help), compact)
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
