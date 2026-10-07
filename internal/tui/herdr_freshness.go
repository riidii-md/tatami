package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
)

func (a *App) currentHubEndpoint(e herdrhub.Endpoint) bool {
	for _, current := range herdrhub.ReachableEndpoints(a.hubEndpoints, a.hubSnapshots) {
		if herdrhub.SameRoute(current, e) {
			return true
		}
	}
	return false
}
func (a *App) nextHubOperation(e herdrhub.Endpoint) uint64 {
	if a.hubOperationGenerations == nil {
		a.hubOperationGenerations = map[string]uint64{}
	}
	a.hubOperationGenerations[e.Key()]++
	return a.hubOperationGenerations[e.Key()]
}
func (a *App) applyHubUpdates(updates []herdrhub.Snapshot) {
	before := herdrhub.ReachableEndpoints(a.hubEndpoints, a.hubSnapshots)
	next := mergeHubSnapshots(a.hubEndpoints, a.hubSnapshots, updates)
	after := herdrhub.ReachableEndpoints(a.hubEndpoints, next)
	previous := map[string]herdrhub.Endpoint{}
	current := map[string]herdrhub.Endpoint{}
	for _, e := range before {
		previous[e.Key()] = e
	}
	for _, e := range after {
		current[e.Key()] = e
	}
	changed := map[string]herdrhub.Endpoint{}
	for key, e := range previous {
		other, ok := current[key]
		if !ok || !herdrhub.SameRoute(e, other) {
			changed[key] = e
		}
	}
	for key, e := range current {
		other, ok := previous[key]
		if !ok || !herdrhub.SameRoute(e, other) {
			changed[key] = e
		}
	}
	for _, e := range changed {
		a.nextHubOperation(e)
	}
	if len(changed) > 0 {
		a.herdrHubAgentGeneration++
		if a.herdrHubAgentCancel != nil {
			a.herdrHubAgentCancel()
		}
	}
	a.hubSnapshots = next
}
func (a *App) invalidateHubHost(id string) {
	a.herdrHubGeneration++
	a.herdrHubAgentGeneration++
	if a.herdrHubCancel != nil {
		a.herdrHubCancel()
	}
	if a.herdrHubAgentCancel != nil {
		a.herdrHubAgentCancel()
	}
	if a.herdrHubInteractiveCancel != nil {
		a.herdrHubInteractiveCancel()
	}
	for _, e := range herdrhub.ReachableEndpoints(a.hubEndpoints, a.hubSnapshots) {
		if e.RootID == id || e.ID == id {
			a.nextHubOperation(e)
		}
	}
	a.hubSnapshots = herdrhub.PurgeRoot(a.hubSnapshots, id)
}
