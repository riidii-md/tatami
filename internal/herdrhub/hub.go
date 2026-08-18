// Package herdrhub owns Tatami's private inventory of local and SSH Herdr endpoints.
package herdrhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const LocalEndpointID = "local"

type EndpointKind string

const (
	EndpointLocal EndpointKind = "local"
	EndpointSSH   EndpointKind = "ssh"
)

type EndpointState string

const (
	StateLoading              EndpointState = "loading"
	StateOnline               EndpointState = "online"
	StateOffline              EndpointState = "offline"
	StateAuthenticationNeeded EndpointState = "authentication-needed"
	StateIncompatible         EndpointState = "incompatible"
	StateStale                EndpointState = "stale"
)

type Endpoint struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Kind   EndpointKind `json:"kind,omitempty"`
	Target string       `json:"target,omitempty"`
}
type SessionKey struct {
	EndpointID  string `json:"endpoint_id"`
	SessionName string `json:"session_name"`
}
type Session struct {
	SessionKey
	Running bool `json:"running"`
	Default bool `json:"default"`
}

// Agent contains only display-safe inventory fields. Pane and terminal content,
// process arguments, and raw command diagnostics are intentionally excluded.
type Agent struct {
	Kind   string
	Status string
	CWD    string
}
type Snapshot struct {
	EndpointID  string        `json:"endpoint_id"`
	State       EndpointState `json:"state"`
	Sessions    []Session     `json:"sessions"`
	LastSuccess time.Time     `json:"last_success,omitempty"`
	Latency     time.Duration `json:"latency,omitempty"`
	Error       string        `json:"-"`
}

func LocalEndpoint() Endpoint {
	return Endpoint{ID: LocalEndpointID, Label: "This Mac", Kind: EndpointLocal}
}
func ValidateEndpoint(e Endpoint) error {
	if e.ID == LocalEndpointID {
		return errors.New("local endpoint is reserved")
	}
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Label) == "" {
		return errors.New("endpoint id and label are required")
	}
	if len(e.ID) > 64 || !asciiAlphaNumeric(rune(e.ID[0])) || !asciiAlphaNumeric(rune(e.ID[len(e.ID)-1])) {
		return errors.New("endpoint id must start and end with a lowercase letter or number")
	}
	for _, r := range e.ID {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return errors.New("endpoint id must be a stable safe slug")
		}
	}
	if err := validateDisplayField("endpoint label", e.Label, 128); err != nil {
		return err
	}
	if e.Kind != "" && e.Kind != EndpointSSH {
		return fmt.Errorf("unsupported endpoint kind %q", e.Kind)
	}
	t := strings.TrimSpace(e.Target)
	if t == "" {
		return errors.New("SSH destination is required")
	}
	if err := validateSSHDestination(t); err != nil {
		return err
	}
	return nil
}

func asciiAlphaNumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func validateSSHDestination(destination string) error {
	if strings.HasPrefix(destination, "ssh://") {
		return validateSSHURI(destination)
	}
	if len(destination) > 320 || strings.HasPrefix(destination, "-") || strings.Count(destination, "@") > 1 {
		return invalidSSHDestination()
	}
	user, host, hasUser := strings.Cut(destination, "@")
	if !hasUser {
		host = user
		user = ""
	}
	if hasUser && !safeSSHNamePart(user, 64, false) {
		return invalidSSHDestination()
	}
	if !safeSSHNamePart(host, 253, true) {
		return invalidSSHDestination()
	}
	return nil
}

func validateSSHURI(destination string) error {
	if len(destination) > 384 {
		return invalidSSHDestination()
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Scheme != "ssh" || parsed.Opaque != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return invalidSSHDestination()
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword || !safeSSHNamePart(parsed.User.Username(), 64, false) {
			return invalidSSHDestination()
		}
	}
	host := parsed.Hostname()
	if net.ParseIP(host) == nil && !safeSSHNamePart(host, 253, true) {
		return invalidSSHDestination()
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return invalidSSHDestination()
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return invalidSSHDestination()
		}
	}
	return nil
}

func invalidSSHDestination() error {
	return errors.New("SSH destination must be a safe alias, host, IP, user@host, or ssh://user@host:port")
}

func safeSSHNamePart(value string, max int, requireAlphaNumericEdges bool) bool {
	if value == "" || len(value) > max {
		return false
	}
	if requireAlphaNumericEdges && (!asciiAlphaNumeric(rune(value[0])) || !asciiAlphaNumeric(rune(value[len(value)-1]))) {
		return false
	}
	for _, r := range value {
		if !asciiAlphaNumeric(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func validateRemoteSessionName(name string) error {
	if name == "" || len(name) > 128 {
		return errors.New("remote Herdr session name must be 1-128 characters")
	}
	for _, r := range name {
		if !asciiAlphaNumeric(r) && r != '-' && r != '_' && r != '.' {
			return errors.New("remote Herdr session name contains unsupported characters")
		}
	}
	return nil
}

func validateDisplayField(name, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s is too long", name)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return fmt.Errorf("%s contains terminal control characters", name)
		}
	}
	return nil
}

type Store struct{ path string }

func NewStore(path string) *Store { return &Store{path: path} }
func (s *Store) List() ([]Endpoint, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Endpoint{LocalEndpoint()}, nil
	}
	if err != nil {
		return nil, err
	}
	var data struct {
		Hosts []Endpoint `json:"hosts"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("read Herdr hosts config: %w", err)
	}
	seen := map[string]bool{LocalEndpointID: true}
	out := []Endpoint{LocalEndpoint()}
	for _, e := range data.Hosts {
		e.Kind = EndpointSSH
		if err := ValidateEndpoint(e); err != nil {
			return nil, err
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("duplicate endpoint id %q", e.ID)
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out, nil
}
func (s *Store) Save(endpoints []Endpoint) error {
	seen := map[string]bool{LocalEndpointID: true}
	hosts := make([]Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		if e.ID == LocalEndpointID {
			continue
		}
		e.Kind = EndpointSSH
		if err := ValidateEndpoint(e); err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("duplicate endpoint id %q", e.ID)
		}
		seen[e.ID] = true
		hosts = append(hosts, e)
	}
	b, err := json.MarshalIndent(struct {
		Hosts []Endpoint `json:"hosts"`
	}{hosts}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.path, append(b, '\n'))
}
func (s *Store) Delete(id string) error {
	if id == LocalEndpointID {
		return errors.New("local endpoint cannot be deleted")
	}
	eps, err := s.List()
	if err != nil {
		return err
	}
	out := make([]Endpoint, 0, len(eps))
	found := false
	for _, e := range eps {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return fmt.Errorf("endpoint %q not found", id)
	}
	return s.Save(out)
}

type Cache struct {
	Version   int        `json:"version"`
	Snapshots []Snapshot `json:"snapshots"`
}

const CacheVersion = 1

func LoadCache(path string) (Cache, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Cache{}, nil
	}
	if err != nil {
		return Cache{}, err
	}
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil {
		return Cache{}, fmt.Errorf("read Herdr hub cache: %w", err)
	}
	if c.Version != CacheVersion {
		return Cache{}, fmt.Errorf("unsupported Herdr hub cache version %d", c.Version)
	}
	if err := validateCache(c); err != nil {
		return Cache{}, fmt.Errorf("read Herdr hub cache: %w", err)
	}
	return c, nil
}
func SaveCache(path string, c Cache) error {
	c.Version = CacheVersion
	if err := validateCache(c); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}

func validateCache(c Cache) error {
	for i := range c.Snapshots {
		snapshot := &c.Snapshots[i]
		switch snapshot.State {
		case StateLoading, StateOnline, StateOffline, StateAuthenticationNeeded, StateIncompatible, StateStale:
		default:
			return fmt.Errorf("unsupported endpoint state %q", snapshot.State)
		}
		for j := range snapshot.Sessions {
			if err := validateRemoteSessionName(snapshot.Sessions[j].SessionName); err != nil {
				return fmt.Errorf("unsafe cached session: %w", err)
			}
			snapshot.Sessions[j].EndpointID = snapshot.EndpointID
		}
	}
	return nil
}
func atomicWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-herdr-hub-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type ExecResult struct{ Stdout, Stderr []byte }
type Executor interface {
	Output(context.Context, string, ...string) (ExecResult, error)
}
type OSExecutor struct{}

type boundedOutput struct {
	data      []byte
	limit     int
	truncated bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	remaining := w.limit - len(w.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		w.data = append(w.data, p[:remaining]...)
	}
	if remaining < len(p) {
		w.truncated = true
	}
	return len(p), nil
}

func (OSExecutor) Output(ctx context.Context, name string, args ...string) (ExecResult, error) {
	stdout := &boundedOutput{limit: 2 << 20}
	stderr := &boundedOutput{limit: 4096}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.truncated && err == nil {
		err = errors.New("Herdr inventory output exceeded 2 MiB")
	}
	return ExecResult{Stdout: stdout.data, Stderr: stderr.data}, err
}

type Client struct{ exec Executor }

func NewClient(e Executor) *Client {
	if e == nil {
		e = OSExecutor{}
	}
	return &Client{exec: e}
}
func QueryArgs(e Endpoint) (string, []string, error) {
	if e.ID == LocalEndpointID || e.Kind == EndpointLocal {
		return "herdr", []string{"session", "list", "--json"}, nil
	}
	if err := ValidateEndpoint(e); err != nil {
		return "", nil, err
	}
	return "ssh", []string{"-o", "BatchMode=yes", "--", e.Target, "herdr", "session", "list", "--json"}, nil
}
func AttachArgs(e Endpoint, session string) (string, []string, error) {
	if strings.TrimSpace(session) == "" {
		return "", nil, errors.New("Herdr session name is required")
	}
	if e.ID == LocalEndpointID || e.Kind == EndpointLocal {
		return "herdr", []string{"--session", session}, nil
	}
	if err := ValidateEndpoint(e); err != nil {
		return "", nil, err
	}
	if err := validateRemoteSessionName(session); err != nil {
		return "", nil, err
	}
	return "herdr", []string{"--remote", e.Target, "--session", session}, nil
}
func AgentArgs(e Endpoint, session string) (string, []string, error) {
	if strings.TrimSpace(session) == "" {
		return "", nil, errors.New("Herdr session name is required")
	}
	if e.ID == LocalEndpointID || e.Kind == EndpointLocal {
		return "herdr", []string{"--session", session, "agent", "list"}, nil
	}
	if err := ValidateEndpoint(e); err != nil {
		return "", nil, err
	}
	if err := validateRemoteSessionName(session); err != nil {
		return "", nil, err
	}
	return "ssh", []string{"-o", "BatchMode=yes", "--", e.Target, "herdr", "--session", session, "agent", "list"}, nil
}
func (c *Client) QueryAgents(ctx context.Context, e Endpoint, session string) ([]Agent, error) {
	name, args, err := AgentArgs(e, session)
	if err != nil {
		return nil, err
	}
	result, err := c.exec.Output(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("agent query %s: %s", stateForError(ctx, err, result.Stderr), "failed")
	}
	return ParseAgents(result.Stdout)
}
func ParseAgents(data []byte) ([]Agent, error) {
	var response struct {
		Result struct {
			Agents []struct {
				Kind   string `json:"agent"`
				Status string `json:"agent_status"`
				CWD    string `json:"cwd"`
				PaneID string `json:"pane_id"`
			} `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse Herdr agents: %w", err)
	}
	out := make([]Agent, 0, len(response.Result.Agents))
	for _, agent := range response.Result.Agents {
		if agent.PaneID == "" {
			continue
		}
		if err := validateDisplayField("agent kind", agent.Kind, 64); err != nil {
			return nil, err
		}
		if err := validateDisplayField("agent status", agent.Status, 64); err != nil {
			return nil, err
		}
		if err := validateDisplayField("agent cwd", agent.CWD, 1024); err != nil {
			return nil, err
		}
		out = append(out, Agent{Kind: agent.Kind, Status: agent.Status, CWD: agent.CWD})
	}
	return out, nil
}
func (c *Client) Query(ctx context.Context, e Endpoint) Snapshot {
	start := time.Now()
	name, args, err := QueryArgs(e)
	if err != nil {
		return Snapshot{EndpointID: e.ID, State: StateIncompatible, Error: err.Error()}
	}
	result, err := c.exec.Output(ctx, name, args...)
	if err != nil {
		return Snapshot{EndpointID: e.ID, State: stateForError(ctx, err, result.Stderr), Latency: time.Since(start), Error: "endpoint query failed"}
	}
	sessions, err := ParseSessions(e.ID, result.Stdout)
	if err != nil {
		return Snapshot{EndpointID: e.ID, State: StateIncompatible, Latency: time.Since(start), Error: err.Error()}
	}
	return Snapshot{EndpointID: e.ID, State: StateOnline, Sessions: sessions, LastSuccess: time.Now().UTC(), Latency: time.Since(start)}
}
func ParseSessions(endpointID string, b []byte) ([]Session, error) {
	var v struct {
		Sessions []struct {
			Name    string `json:"name"`
			Running bool   `json:"running"`
			Default bool   `json:"default"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("parse Herdr sessions: %w", err)
	}
	out := make([]Session, 0, len(v.Sessions))
	for _, x := range v.Sessions {
		if strings.TrimSpace(x.Name) != "" {
			if err := validateRemoteSessionName(x.Name); err != nil {
				return nil, fmt.Errorf("parse Herdr session name: %w", err)
			}
			out = append(out, Session{SessionKey: SessionKey{EndpointID: endpointID, SessionName: x.Name}, Running: x.Running, Default: x.Default})
		}
	}
	return out, nil
}
func stateForError(ctx context.Context, err error, stderr []byte) EndpointState {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return StateOffline
	}
	m := strings.ToLower(err.Error() + " " + string(stderr))
	if strings.Contains(m, "permission denied") || strings.Contains(m, "authentication") {
		return StateAuthenticationNeeded
	}
	if strings.Contains(m, "not found") || strings.Contains(m, "command not found") {
		return StateIncompatible
	}
	return StateOffline
}

// Refresh queries endpoints independently, using a bounded worker pool. A
// previous successful snapshot is retained on failure so callers can render it
// as stale instead of losing an entire endpoint group.
func Refresh(ctx context.Context, client *Client, endpoints []Endpoint, previous Cache, limit int) []Snapshot {
	return RefreshWithTimeout(ctx, client, endpoints, previous, limit, 5*time.Second)
}

// RefreshWithTimeout applies an independent deadline to each endpoint; one
// stalled SSH process cannot consume the hub's entire refresh budget.
func RefreshWithTimeout(ctx context.Context, client *Client, endpoints []Endpoint, previous Cache, limit int, timeout time.Duration) []Snapshot {
	if limit < 1 {
		limit = 1
	}
	prior := make(map[string]Snapshot, len(previous.Snapshots))
	for _, snapshot := range previous.Snapshots {
		prior[snapshot.EndpointID] = snapshot
	}
	type indexedEndpoint struct {
		index    int
		endpoint Endpoint
	}
	type indexedSnapshot struct {
		index    int
		snapshot Snapshot
	}
	jobs := make(chan indexedEndpoint)
	results := make(chan indexedSnapshot, len(endpoints))
	for range limit {
		go func() {
			for job := range jobs {
				endpointCtx, cancel := context.WithTimeout(ctx, timeout)
				snapshot := client.Query(endpointCtx, job.endpoint)
				cancel()
				if snapshot.State != StateOnline {
					if old, ok := prior[job.endpoint.ID]; ok && len(old.Sessions) > 0 {
						snapshot.Sessions = old.Sessions
						snapshot.LastSuccess = old.LastSuccess
						snapshot.State = StateStale
					}
				}
				results <- indexedSnapshot{index: job.index, snapshot: snapshot}
			}
		}()
	}
	go func() {
		for index, endpoint := range endpoints {
			jobs <- indexedEndpoint{index: index, endpoint: endpoint}
		}
		close(jobs)
	}()
	out := make([]Snapshot, len(endpoints))
	for range endpoints {
		result := <-results
		out[result.index] = result.snapshot
	}
	return out
}
