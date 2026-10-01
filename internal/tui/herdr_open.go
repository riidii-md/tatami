package tui

import (
	"fmt"
	"strings"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// HerdrOpenMode chooses whether a target gets a dedicated session or joins an existing one.
type HerdrOpenMode int

const (
	HerdrOpenDedicated HerdrOpenMode = iota
	HerdrOpenExisting
)

// HerdrOpenModeView lets the user choose where a Herdr target is opened.
type HerdrOpenModeView struct {
	cursor     int
	search     searchController
	mobileMode bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (v *HerdrOpenModeView) SetMobileMode(enabled bool) {
	v.mobileMode = enabled
}

func NewHerdrOpenModeView() *HerdrOpenModeView {
	view := &HerdrOpenModeView{search: newSearchController("Herdr destinations")}
	view.rebuildSearch()
	return view
}

func (v *HerdrOpenModeView) Selected() HerdrOpenMode {
	id, ok := v.search.ActiveDocumentID()
	if !ok || id == herdrOpenModeSearchID(HerdrOpenDedicated) {
		return HerdrOpenDedicated
	}
	return HerdrOpenExisting
}

func (v *HerdrOpenModeView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	event, cmd := v.handleKey(key)
	if event.Consumed {
		return cmd
	}
	switch key.String() {
	case "j":
		v.search.Move(1)
	case "k":
		v.search.Move(-1)
	default:
		if v.mobileMode {
			v.search.SelectVisibleChoice(key.String(), 7, 1)
		}
	}
	v.cursor = v.search.ActiveIndex()
	return nil
}

func herdrOpenModeSearchID(mode HerdrOpenMode) appsearch.ID {
	return appsearch.ID(fmt.Sprintf("herdr-open:%d", mode))
}

func (v *HerdrOpenModeView) rebuildSearch() {
	labels := herdrOpenModeLabels()
	documents := make([]appsearch.Document, 0, len(labels))
	rows := make([]searchRow, 0, len(labels))
	for index, label := range labels {
		id := herdrOpenModeSearchID(HerdrOpenMode(index))
		documents = append(documents, appsearch.Document{ID: id, Kind: "herdr-destination", Primary: label, Ordinal: index})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = v.search.ReplaceDocuments(1, documents, rows)
}

func (v *HerdrOpenModeView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	event, cmd := v.search.Update(msg)
	v.cursor = v.search.ActiveIndex()
	return event, cmd
}

func herdrOpenModeLabels() []string {
	return []string{"new / separate herdr session", "existing herdr session..."}
}

func (v *HerdrOpenModeView) View() string {
	labels := herdrOpenModeLabels()
	var b strings.Builder
	b.WriteString(titleStyle.Render("Open in Herdr"))
	b.WriteString("\n")
	b.WriteString(v.search.QueryView())
	b.WriteString("\n\n")
	start, rows := v.search.VisibleRows(7, 1)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		mode := HerdrOpenDedicated
		if row.DocumentID == herdrOpenModeSearchID(HerdrOpenExisting) {
			mode = HerdrOpenExisting
		}
		label := labels[int(mode)]
		cursor := choicePrefix(v.mobileMode, visibleIndex, i == v.cursor)
		style := normalStyle
		if i == v.cursor {
			style = selectedStyle
		}
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	if len(v.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No matching destinations."))
		b.WriteString("\n")
	}
	help := "\n[type]search  [↓]browse  [enter]select  [esc]clear/back"
	if v.mobileMode {
		help = "\n[type]search  [↓]browse  [1-9]select in browse  [enter]open"
	}
	b.WriteString(helpStyle.Render(help))
	return renderPanel(b.String(), v.mobileMode)
}

// HerdrSessionNameView lets the user accept or replace a generated session name.
type HerdrSessionNameView struct {
	input      textinput.Model
	mobileMode bool
}

// SetMobileMode enables compact input rendering.
func (v *HerdrSessionNameView) SetMobileMode(enabled bool) {
	v.mobileMode = enabled
}

func NewHerdrSessionNameView(defaultName string) *HerdrSessionNameView {
	input := textinput.New()
	input.Placeholder = "session-name"
	input.CharLimit = 80
	input.Width = 40
	input.SetValue(defaultName)
	input.Focus()
	return &HerdrSessionNameView{input: input}
}

func (v *HerdrSessionNameView) Value() string {
	return strings.TrimSpace(v.input.Value())
}

func (v *HerdrSessionNameView) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

func (v *HerdrSessionNameView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Name New Herdr Session"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("Session Name"))
	b.WriteString("\n")
	b.WriteString(v.input.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("\n[enter]open  [esc]back"))
	return renderPanel(b.String(), v.mobileMode)
}

// HerdrSessionPickerView lets the user select any known Herdr session.
type HerdrSessionPickerView struct {
	sessions       []shell.HerdrSession
	cursor         int
	search         searchController
	currentSession string
	err            error
	mobileMode     bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (v *HerdrSessionPickerView) SetMobileMode(enabled bool) {
	v.mobileMode = enabled
}

func NewHerdrSessionPickerView(sessions []shell.HerdrSession, currentSession string, err error) *HerdrSessionPickerView {
	ordered := make([]shell.HerdrSession, 0, len(sessions)+1)
	currentFound := false
	for _, session := range sessions {
		if session.Name == currentSession && currentSession != "" {
			ordered = append(ordered, session)
			currentFound = true
		}
	}
	if currentSession != "" && !currentFound {
		ordered = append(ordered, shell.HerdrSession{Name: currentSession, Running: true})
	}
	for _, session := range sessions {
		if session.Name != currentSession {
			ordered = append(ordered, session)
		}
	}
	view := &HerdrSessionPickerView{
		sessions:       ordered,
		currentSession: currentSession,
		err:            err,
		search:         newSearchController("Herdr sessions"),
	}
	view.rebuildSearch()
	return view
}

func (v *HerdrSessionPickerView) Selected() string {
	id, ok := v.search.ActiveDocumentID()
	if ok {
		for _, session := range v.sessions {
			if herdrSessionSearchID(session.Name) == id {
				return session.Name
			}
		}
	}
	return ""
}

func (v *HerdrSessionPickerView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	event, cmd := v.handleKey(key)
	if event.Consumed {
		return cmd
	}
	switch key.String() {
	case "j":
		v.search.Move(1)
	case "k":
		v.search.Move(-1)
	case "g":
		v.search.SelectFirst()
	case "G":
		v.search.SelectLast()
	default:
		if v.mobileMode {
			v.search.SelectVisibleChoice(key.String(), 8, 1)
		}
	}
	v.cursor = v.search.ActiveIndex()
	return nil
}

func herdrSessionSearchID(name string) appsearch.ID { return appsearch.ID("herdr-session:" + name) }

func (v *HerdrSessionPickerView) rebuildSearch() {
	documents := make([]appsearch.Document, 0, len(v.sessions))
	rows := make([]searchRow, 0, len(v.sessions))
	for index, session := range v.sessions {
		id := herdrSessionSearchID(session.Name)
		state := "stopped"
		boost := 0
		if session.Name == v.currentSession {
			state, boost = "current running", 3
		} else if session.Running {
			state, boost = "running", 2
		}
		documents = append(documents, appsearch.Document{ID: id, Kind: "herdr-session", Primary: session.Name, Fields: []appsearch.Field{{Name: "status", Value: state, Class: appsearch.MetadataField}}, Ordinal: index, Boost: boost})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = v.search.ReplaceDocuments(1, documents, rows)
}

func (v *HerdrSessionPickerView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	event, cmd := v.search.Update(msg)
	v.cursor = v.search.ActiveIndex()
	return event, cmd
}

func (v *HerdrSessionPickerView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Choose Herdr Session"))
	b.WriteString("\n")
	b.WriteString(v.search.QueryView())
	b.WriteString("\n\n")
	if v.err != nil {
		b.WriteString(errorStyle.Render("Could not list Herdr sessions: " + v.err.Error()))
		b.WriteString("\n")
	} else if len(v.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No existing Herdr sessions"))
		b.WriteString("\n")
	} else {
		start, rows := v.search.VisibleRows(8, 1)
		for visibleIndex, row := range rows {
			i := start + visibleIndex
			session, ok := v.sessionByID(row.DocumentID)
			if !ok {
				continue
			}
			cursor := choicePrefix(v.mobileMode, visibleIndex, i == v.cursor)
			style := normalStyle
			if i == v.cursor {
				style = selectedStyle
			}
			label := session.Name
			if session.Name == v.currentSession {
				label += " (current)"
			} else if session.Running {
				label += " (running)"
			} else {
				label += " (stopped)"
			}
			b.WriteString(cursor)
			b.WriteString(style.Render(label))
			b.WriteString("\n")
		}
	}
	if v.search.Truncated() {
		b.WriteString(mutedStyle.Render("Results truncated."))
		b.WriteString("\n")
	}
	help := "\n[type]search  [↓]browse  [enter]select  [esc]clear/back"
	if v.mobileMode {
		help = "\n[type]search  [↓]browse  [1-9]select in browse  [enter]open"
	}
	b.WriteString(helpStyle.Render(help))
	return renderPanel(b.String(), v.mobileMode)
}

func (v *HerdrSessionPickerView) sessionByID(id appsearch.ID) (shell.HerdrSession, bool) {
	for _, session := range v.sessions {
		if herdrSessionSearchID(session.Name) == id {
			return session, true
		}
	}
	return shell.HerdrSession{}, false
}
