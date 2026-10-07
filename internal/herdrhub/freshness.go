package herdrhub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func RouteFingerprint(e Endpoint) string {
	h := sha256.New()
	values := append([]string{e.RootRevision}, e.Via...)
	values = append(values, e.Target)
	for _, value := range values {
		fmt.Fprintf(h, "%d:%s", len(value), value)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func StampSnapshot(e Endpoint, s Snapshot) Snapshot {
	s.EndpointID = e.Key()
	s.RootID = e.RootID
	s.RootRevision = e.RootRevision
	s.RouteFingerprint = RouteFingerprint(e)
	return s
}
func SnapshotMatches(e Endpoint, s Snapshot) bool {
	if s.EndpointID != e.Key() {
		return false
	}
	// Unbound endpoints are legacy in-process callers, never profiles loaded from disk.
	if e.RootID == "" && e.RootRevision == "" {
		return s.RootID == "" && s.RootRevision == "" && (s.RouteFingerprint == "" || s.RouteFingerprint == RouteFingerprint(e))
	}
	return s.RootID == e.RootID && s.RootRevision == e.RootRevision && s.RouteFingerprint == RouteFingerprint(e)
}
func ReconcileSnapshots(roots []Endpoint, snapshots []Snapshot, restoring bool) []Snapshot {
	byID := map[string]Snapshot{}
	for _, s := range snapshots {
		byID[s.EndpointID] = s
	}
	out := []Snapshot{}
	visited := map[string]bool{}
	var visit func(Endpoint)
	visit = func(e Endpoint) {
		if visited[e.Key()] || e.ID == LocalEndpointID {
			return
		}
		visited[e.Key()] = true
		s, ok := byID[e.Key()]
		if !ok || !SnapshotMatches(e, s) {
			return
		}
		if restoring && s.State == StateOnline {
			s.State = StateStale
		}
		out = append(out, s)
		if s.State != StateOnline && s.State != StateStale {
			return
		}
		for _, ad := range s.Hosts {
			child, err := DescendantEndpoint(e, ad)
			if err == nil {
				visit(child)
			}
		}
	}
	for _, e := range roots {
		visit(e)
	}
	return out
}
func ReachableEndpoints(roots []Endpoint, snapshots []Snapshot) []Endpoint {
	safe := ReconcileSnapshots(roots, snapshots, false)
	byID := map[string]Snapshot{}
	for _, s := range safe {
		byID[s.EndpointID] = s
	}
	out := []Endpoint{}
	visited := map[string]bool{}
	var visit func(Endpoint)
	visit = func(e Endpoint) {
		if visited[e.Key()] {
			return
		}
		visited[e.Key()] = true
		out = append(out, e)
		s, ok := byID[e.Key()]
		if !ok {
			return
		}
		for _, ad := range s.Hosts {
			child, err := DescendantEndpoint(e, ad)
			if err == nil {
				visit(child)
			}
		}
	}
	for _, e := range roots {
		visit(e)
	}
	return out
}
func PurgeRoot(snapshots []Snapshot, id string) []Snapshot {
	out := []Snapshot{}
	for _, s := range snapshots {
		if s.EndpointID != id && !strings.HasPrefix(s.EndpointID, id+"/") {
			out = append(out, s)
		}
	}
	return out
}
func SameRoute(a, b Endpoint) bool {
	return a.Key() == b.Key() && a.RootID == b.RootID && a.RootRevision == b.RootRevision && RouteFingerprint(a) == RouteFingerprint(b)
}
func JumpDestination(e Endpoint) (string, error) {
	if e.Profile != nil {
		c := e.Profile.Connection
		if c.JumpAlias != "" {
			return c.JumpAlias, nil
		}
		if c.Mode == ModeExplicit && (c.Auth != "" && c.Auth != "config-agent" || c.IdentityFile != "" || c.CertificateFile != "") {
			return "", fmt.Errorf("configure an OpenSSH jump alias for %s before opening descendants; root credentials apply only to the root", e.Label)
		}
	}
	return e.Target, nil
}
