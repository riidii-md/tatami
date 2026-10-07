package sshconn

// Origin is non-secret provenance carried across the TUI handoff.
type Origin struct {
	RootID, RootRevision, EndpointKey, RouteFingerprint, Target string
	Via                                                         []string
}
