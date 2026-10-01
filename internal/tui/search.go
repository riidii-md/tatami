package tui

import (
	"fmt"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type searchFocus uint8

const (
	searchFocusQuery searchFocus = iota
	searchFocusBrowse
)

type searchRow struct {
	RowID      appsearch.ID
	DocumentID appsearch.ID
}

type searchEvent struct {
	Consumed               bool
	Activate               bool
	BrowseSelectionChanged bool
}

type searchController struct {
	input        textinput.Model
	focus        searchFocus
	catalog      appsearch.Catalog
	emptyRows    []searchRow
	rows         []searchRow
	matches      map[appsearch.ID]appsearch.Match
	active       int
	truncated    bool
	generation   uint64
	height       int
	visibleStart int
}

func newSearchController(placeholder string) searchController {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 200
	input.Prompt = "Search: "
	input.Focus()
	catalog, _ := appsearch.NewCatalog(nil, appsearch.MaxDocuments)
	return searchController{
		input:   input,
		focus:   searchFocusQuery,
		catalog: catalog,
		matches: make(map[appsearch.ID]appsearch.Match),
	}
}

func (c *searchController) ReplaceDocuments(generation uint64, documents []appsearch.Document, emptyRows []searchRow) error {
	if generation < c.generation {
		return nil
	}
	catalog, err := appsearch.NewCatalog(documents, appsearch.MaxDocuments)
	if err != nil {
		return fmt.Errorf("compile search catalog: %w", err)
	}
	previousRow, previousDocument := c.activeIDs()
	c.catalog = catalog
	c.emptyRows = append([]searchRow(nil), emptyRows...)
	c.generation = generation
	c.rebuild(previousRow, previousDocument)
	return nil
}

func (c *searchController) Update(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return searchEvent{}, nil
	}

	if c.focus == searchFocusBrowse {
		switch key {
		case "down":
			return c.move(1), nil
		case "up":
			if c.active <= 0 {
				c.setFocus(searchFocusQuery)
				return searchEvent{Consumed: true}, nil
			}
			return c.move(-1), nil
		case "/":
			c.setFocus(searchFocusQuery)
			return searchEvent{Consumed: true}, nil
		case "enter":
			return searchEvent{Consumed: true, Activate: len(c.rows) > 0}, nil
		case "esc":
			if c.input.Value() == "" {
				return searchEvent{}, nil
			}
			c.input.SetValue("")
			c.setFocus(searchFocusQuery)
			c.rebuild("", "")
			return searchEvent{Consumed: true}, nil
		default:
			return searchEvent{}, nil
		}
	}
	if key == "/" && c.input.Value() == "" {
		return searchEvent{Consumed: true}, nil
	}

	switch key {
	case "down":
		if len(c.rows) > 0 {
			c.active = 0
			c.setFocus(searchFocusBrowse)
			return searchEvent{Consumed: true, BrowseSelectionChanged: true}, nil
		}
		return searchEvent{Consumed: true}, nil
	case "enter":
		return searchEvent{Consumed: true, Activate: len(c.rows) > 0}, nil
	case "esc":
		if c.input.Value() == "" {
			return searchEvent{}, nil
		}
		c.input.SetValue("")
		c.rebuild("", "")
		return searchEvent{Consumed: true}, nil
	}

	previous := c.input.Value()
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	if c.input.Value() != previous {
		c.rebuild("", "")
	}
	return searchEvent{Consumed: true}, cmd
}

func (c *searchController) Move(delta int) searchEvent {
	if c.focus != searchFocusBrowse {
		return searchEvent{}
	}
	return c.move(delta)
}

func (c *searchController) SelectIndex(index int) searchEvent {
	if index < 0 || index >= len(c.rows) {
		return searchEvent{}
	}
	changed := c.active != index || c.focus != searchFocusBrowse
	c.active = index
	c.setFocus(searchFocusBrowse)
	return searchEvent{Consumed: true, BrowseSelectionChanged: changed}
}

func (c *searchController) SelectRowID(rowID appsearch.ID) searchEvent {
	for index, row := range c.rows {
		if row.RowID == rowID {
			return c.SelectIndex(index)
		}
	}
	return searchEvent{}
}

func (c *searchController) SelectVisibleChoice(key string, reserved, rowHeight int) searchEvent {
	start, rows := c.VisibleRows(reserved, rowHeight)
	index, ok := numberKeyIndex(key, min(len(rows), 9))
	if !ok {
		return searchEvent{}
	}
	return c.SelectIndex(start + index)
}

func (c *searchController) SelectFirst() searchEvent {
	return c.SelectIndex(0)
}

func (c *searchController) SelectLast() searchEvent {
	return c.SelectIndex(len(c.rows) - 1)
}

func (c *searchController) FocusQuery() {
	c.setFocus(searchFocusQuery)
}

func (c *searchController) Clear() {
	c.input.SetValue("")
	c.setFocus(searchFocusQuery)
	c.rebuild("", "")
}

func (c *searchController) move(delta int) searchEvent {
	if len(c.rows) == 0 {
		return searchEvent{Consumed: true}
	}
	next := clampInt(c.active+delta, 0, len(c.rows)-1)
	changed := next != c.active
	c.active = next
	return searchEvent{Consumed: true, BrowseSelectionChanged: changed}
}

func (c *searchController) rebuild(previousRow, previousDocument appsearch.ID) {
	c.matches = make(map[appsearch.ID]appsearch.Match)
	if c.input.Value() == "" {
		c.rows = append(c.rows[:0], c.emptyRows...)
		c.truncated = len(c.emptyRows) > appsearch.MaxDocuments
		if c.truncated {
			c.rows = c.rows[:appsearch.MaxDocuments]
		}
	} else {
		results := c.catalog.Match(c.input.Value(), appsearch.MaxResults)
		c.rows = make([]searchRow, len(results.Matches))
		for i, match := range results.Matches {
			c.rows[i] = searchRow{RowID: match.ID, DocumentID: match.ID}
			c.matches[match.ID] = match
		}
		c.truncated = results.Truncated
	}

	c.active = 0
	for index, row := range c.rows {
		if previousRow != "" && row.RowID == previousRow {
			c.active = index
			return
		}
	}
	for index, row := range c.rows {
		if previousDocument != "" && row.DocumentID == previousDocument {
			c.active = index
			return
		}
	}
}

func (c *searchController) setActiveIndex(index int) {
	if len(c.rows) == 0 {
		c.active = 0
		return
	}
	c.active = clampInt(index, 0, len(c.rows)-1)
}

func (c *searchController) SetHeight(height int) {
	c.height = max(0, height)
}

func (c *searchController) VisibleRows(reserved, rowHeight int) (int, []searchRow) {
	if len(c.rows) == 0 {
		c.visibleStart = 0
		return 0, nil
	}
	if rowHeight < 1 {
		rowHeight = 1
	}
	limit := len(c.rows)
	if c.height > 0 {
		limit = max(1, (c.height-reserved)/rowHeight)
		limit = min(limit, len(c.rows))
	}
	start := clampInt(c.visibleStart, 0, max(0, len(c.rows)-limit))
	if c.active < start {
		start = c.active
	} else if c.active >= start+limit {
		start = c.active - limit + 1
	}
	start = clampInt(start, 0, max(0, len(c.rows)-limit))
	c.visibleStart = start
	return start, c.rows[start : start+limit]
}

func (c *searchController) setFocus(focus searchFocus) {
	c.focus = focus
	if focus == searchFocusQuery {
		c.input.Focus()
	} else {
		c.input.Blur()
	}
}

func (c *searchController) activeIDs() (appsearch.ID, appsearch.ID) {
	if c.active < 0 || c.active >= len(c.rows) {
		return "", ""
	}
	return c.rows[c.active].RowID, c.rows[c.active].DocumentID
}

func (c *searchController) ActiveDocumentID() (appsearch.ID, bool) {
	_, documentID := c.activeIDs()
	return documentID, documentID != ""
}

func (c *searchController) ActiveIndex() int   { return c.active }
func (c *searchController) Rows() []searchRow  { return c.rows }
func (c *searchController) Query() string      { return c.input.Value() }
func (c *searchController) QueryView() string  { return c.input.View() }
func (c *searchController) InQueryFocus() bool { return c.focus == searchFocusQuery }
func (c *searchController) Truncated() bool    { return c.truncated }

func (c *searchController) MatchFor(id appsearch.ID) (appsearch.Match, bool) {
	match, ok := c.matches[id]
	return match, ok
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
