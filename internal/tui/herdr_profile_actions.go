package tui

import (
	"context"
	"errors"
	"reflect"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	tea "github.com/charmbracelet/bubbletea"
)

type herdrHostTestResultMsg struct {
	View     *HerdrHostView
	Profile  herdrhub.SavedHost
	Snapshot herdrhub.Snapshot
	Err      error
}

func (a *App) hubProfiles() []herdrhub.SavedHost {
	profiles := []herdrhub.SavedHost{}
	for _, e := range a.hubEndpoints {
		if e.ID == herdrhub.LocalEndpointID {
			continue
		}
		p := herdrhub.LegacyProfile(e)
		if e.Profile != nil {
			p = *e.Profile
		}
		profiles = append(profiles, p)
	}
	return profiles
}
func (a *App) persistHubProfiles(profiles []herdrhub.SavedHost) ([]herdrhub.SavedHost, error) {
	if a.herdrHubProfileSaver != nil {
		return a.herdrHubProfileSaver(profiles)
	}
	if a.herdrHubEndpointSaver == nil {
		return nil, errors.New("host saving is unavailable")
	}
	old := map[string]herdrhub.SavedHost{}
	for _, p := range a.hubProfiles() {
		old[p.ID] = p
	}
	saved := make([]herdrhub.SavedHost, len(profiles))
	copy(saved, profiles)
	endpoints := []herdrhub.Endpoint{herdrhub.LocalEndpoint()}
	for i, p := range saved {
		prior, ok := old[p.ID]
		if !ok || prior.Target != p.Target || !reflect.DeepEqual(prior.Connection, p.Connection) {
			var err error
			p.ConnectivityRevision, err = herdrhub.NewConnectivityRevision()
			if err != nil {
				return nil, err
			}
		} else {
			p.ConnectivityRevision = prior.ConnectivityRevision
		}
		saved[i] = p
		e, err := p.Endpoint()
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, e)
	}
	if err := a.herdrHubEndpointSaver(endpoints); err != nil {
		return nil, err
	}
	return saved, nil
}
func (a *App) replaceHubProfiles(profiles []herdrhub.SavedHost) {
	previous := map[string]herdrhub.Endpoint{}
	for _, e := range a.hubEndpoints {
		previous[e.ID] = e
	}
	endpoints := []herdrhub.Endpoint{herdrhub.LocalEndpoint()}
	for _, p := range profiles {
		e, err := p.Endpoint()
		if err != nil {
			continue
		}
		endpoints = append(endpoints, e)
		old, exists := previous[e.ID]
		if exists && !herdrhub.SameRoute(old, e) {
			a.invalidateHubHost(e.ID)
		}
		delete(previous, e.ID)
	}
	for id := range previous {
		if id != herdrhub.LocalEndpointID {
			a.invalidateHubHost(id)
		}
	}
	a.hubEndpoints = endpoints
	a.hubSnapshots = herdrhub.ReconcileSnapshots(endpoints, a.hubSnapshots, false)
}
func (a *App) saveHubCacheSafely() {
	if a.herdrHubCacheSaver != nil {
		if err := a.herdrHubCacheSaver(herdrhub.Cache{Snapshots: a.hubSnapshots}); err != nil {
			a.listView.hubNotice = "Cache could not be saved. Current invalidation remains safe; retry refresh to persist it."
		}
	}
}
func (a *App) startDraftHostTest(p herdrhub.SavedHost) tea.Cmd {
	v := a.herdrHostView
	if a.herdrHubInteractiveInventory == nil {
		v.err = errors.New("interactive SSH Test is unavailable")
		return nil
	}
	if err := p.CheckIdentityFiles(); err != nil {
		v.err = err
		return nil
	}
	e, err := p.Endpoint()
	if err != nil {
		v.err = err
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	if a.herdrHubInteractiveCancel != nil {
		a.herdrHubInteractiveCancel()
	}
	a.herdrHubInteractiveCancel = cancel
	command := &herdrHubInteractiveInventoryCommand{ctx: ctx, endpoint: e, query: a.herdrHubInteractiveInventory}
	v.err = nil
	v.notice = "Testing with OpenSSH…"
	return tea.Exec(command, func(err error) tea.Msg {
		cancel()
		return herdrHostTestResultMsg{View: v, Profile: p, Snapshot: command.snapshot, Err: err}
	})
}
func (a *App) updateHerdrHost(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := a.herdrHostView
	if v == nil {
		a.currentView = ViewList
		return a, nil
	}
	if msg.String() == "esc" || msg.String() == "enter" && v.focus == hostCancel {
		a.herdrHostView = nil
		a.currentView = ViewList
		return a, nil
	}
	if msg.String() != "enter" || v.focus < hostSave {
		return a, v.Update(msg)
	}
	profiles := a.hubProfiles()
	p, err := v.Profile(profiles)
	if err != nil {
		v.err = err
		return a, nil
	}
	if v.focus == hostTest {
		return a, a.startDraftHostTest(p)
	}
	replaced := false
	for i, old := range profiles {
		if old.ID == a.herdrHostEditingID {
			profiles[i] = p
			replaced = true
		}
	}
	if !replaced {
		profiles = append(profiles, p)
	}
	saved, err := a.persistHubProfiles(profiles)
	if err != nil {
		v.err = err
		return a, nil
	}
	a.replaceHubProfiles(saved)
	if v.testSnapshot != nil && v.testedProfile != nil && p.Target == v.testedProfile.Target && reflect.DeepEqual(p.Connection, v.testedProfile.Connection) {
		for _, e := range a.hubEndpoints {
			if e.ID == p.ID {
				a.applyHubUpdates([]herdrhub.Snapshot{herdrhub.StampSnapshot(e, *v.testSnapshot)})
			}
		}
	}
	a.saveHubCacheSafely()
	a.listView.SetHerdrHubSnapshots(a.hubEndpoints, a.hubSnapshots)
	a.herdrHostView = nil
	a.currentView = ViewList
	return a, nil
}
func (a *App) updateHerdrHostDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := a.herdrHostDeleteView
	if v == nil {
		a.currentView = ViewList
		return a, nil
	}
	switch msg.String() {
	case "esc", "q":
		a.herdrHostDeleteView = nil
		a.currentView = ViewList
		return a, nil
	case "enter":
		if !v.Confirmed() {
			a.herdrHostDeleteView = nil
			a.currentView = ViewList
			return a, nil
		}
		out := []herdrhub.SavedHost{}
		for _, p := range a.hubProfiles() {
			if p.ID != v.endpoint.ID {
				out = append(out, p)
			}
		}
		saved, err := a.persistHubProfiles(out)
		if err != nil {
			a.listView.hubNotice = "Host removal could not be saved; configuration is unchanged."
			a.currentView = ViewList
			return a, nil
		}
		a.replaceHubProfiles(saved)
		a.saveHubCacheSafely()
		a.listView.SetHerdrHubSnapshots(a.hubEndpoints, a.hubSnapshots)
		a.herdrHostDeleteView = nil
		a.currentView = ViewList
		return a, nil
	default:
		return a, v.Update(msg)
	}
}
