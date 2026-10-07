package herdrhub

import (
	"errors"
	"slices"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/sshconn"
)

func ResolveOrigin(store *Store, source *sshconn.Origin) (Endpoint, error) {
	if source == nil {
		return Endpoint{}, errors.New("SSH origin is required")
	}
	p, err := store.Resolve(source.RootID)
	if err != nil {
		return Endpoint{}, err
	}
	if p.ConnectivityRevision != source.RootRevision {
		return Endpoint{}, errors.New("saved SSH connection changed; reopen Tatami before launching this item")
	}
	root, err := p.Endpoint()
	if err != nil {
		return Endpoint{}, err
	}
	key := source.EndpointKey
	if key == root.Key() {
		if source.Target != root.Target || !slices.Equal(source.Via, root.Via) || source.RouteFingerprint != RouteFingerprint(root) {
			return Endpoint{}, errors.New("saved root route changed; reopen Tatami")
		}
		return root, nil
	}
	if !strings.HasPrefix(key, root.ID+"/") {
		return Endpoint{}, errors.New("SSH origin does not belong to its saved root")
	}
	parts := strings.Split(key, "/")
	if len(parts) > MaxRouteDepth {
		return Endpoint{}, errors.New("SSH origin route is too deep")
	}
	for _, id := range parts {
		if err := ValidateEndpoint(Endpoint{ID: id, Label: "Origin", Target: "validation"}); err != nil {
			return Endpoint{}, errors.New("SSH origin identity is invalid")
		}
	}
	hop, err := JumpDestination(root)
	if err != nil {
		return Endpoint{}, err
	}
	prefix := append(append([]string(nil), root.Via...), hop)
	if len(source.Via) < len(prefix) || !slices.Equal(source.Via[:len(prefix)], prefix) {
		return Endpoint{}, errors.New("SSH origin route no longer matches its root")
	}
	e := Endpoint{ID: parts[len(parts)-1], NodeID: key, Label: parts[len(parts)-1], Target: source.Target, Via: append([]string(nil), source.Via...), RootID: root.RootID, RootRevision: root.RootRevision}
	if err := validateRoutedEndpoint(e); err != nil {
		return Endpoint{}, errors.New("SSH origin route is invalid")
	}
	if source.RouteFingerprint != RouteFingerprint(e) {
		return Endpoint{}, errors.New("SSH origin route fingerprint changed")
	}
	return e, nil
}
func WorkspaceConnection(e Endpoint, target string, jump []string) (sshconn.Connection, error) {
	if target == e.Target && slices.Equal(jump, e.Via) {
		return endpointConnection(e), nil
	}
	hop, err := JumpDestination(e)
	if err != nil {
		return sshconn.Connection{}, err
	}
	prefix := append(append([]string(nil), e.Via...), hop)
	if len(jump) < len(prefix) || !slices.Equal(jump[:len(prefix)], prefix) {
		return sshconn.Connection{}, errors.New("workspace route does not match its selected endpoint")
	}
	c := sshconn.Connection{Destination: target, Jump: append([]string(nil), jump...)}
	return c, sshconn.Validate(c)
}
