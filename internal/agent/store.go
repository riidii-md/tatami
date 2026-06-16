package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	StatusRunning = "running"
	StatusExited  = "exited"
	StatusStale   = "stale"
	StatusUnknown = "unknown"
)

// Context captures the terminal/multiplexer location where an AI CLI was started.
type Context struct {
	Mux           string `json:"mux"`
	TTY           string `json:"tty,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	ZellijSession string `json:"zellij_session,omitempty"`
	ZellijPaneID  string `json:"zellij_pane_id,omitempty"`
	TmuxSession   string `json:"tmux_session,omitempty"`
	TmuxWindow    string `json:"tmux_window,omitempty"`
	TmuxPane      string `json:"tmux_pane,omitempty"`
}

// Session is one Tatami-tracked AI agent process.
type Session struct {
	ID        string     `json:"id"`
	Agent     string     `json:"agent"`
	Args      []string   `json:"args,omitempty"`
	Command   []string   `json:"command"`
	Cwd       string     `json:"cwd"`
	PID       int        `json:"pid"`
	Status    string     `json:"status"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Context   Context    `json:"context"`
}

// Store persists tracked agent sessions as local private JSON.
type Store struct {
	path string
}

func NewStore(path string) *Store { return &Store{path: path} }

func NewSession(agentName string, args []string, cwd string, ctx Context) *Session {
	now := time.Now().UTC()
	id := fmt.Sprintf("%s-%s", now.Format("20060102-150405"), sanitizeIDPart(agentName))
	cmd := append([]string{agentName}, args...)
	return &Session{
		ID:        id,
		Agent:     agentName,
		Args:      append([]string(nil), args...),
		Command:   cmd,
		Cwd:       cwd,
		Status:    StatusRunning,
		StartedAt: now,
		Context:   ctx,
	}
}

func sanitizeIDPart(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "agent"
	}
	return b.String()
}

func DetectContext() Context {
	tty := ""
	if out, err := os.Readlink("/proc/self/fd/0"); err == nil {
		tty = out
	}
	host, _ := os.Hostname()
	ctx := Context{TTY: tty, Hostname: host, Mux: "none"}

	if os.Getenv("ZELLIJ") != "" || os.Getenv("ZELLIJ_PANE_ID") != "" || os.Getenv("ZELLIJ_SESSION_NAME") != "" {
		ctx.Mux = "zellij"
		ctx.ZellijSession = os.Getenv("ZELLIJ_SESSION_NAME")
		ctx.ZellijPaneID = os.Getenv("ZELLIJ_PANE_ID")
		return ctx
	}
	if os.Getenv("TMUX") != "" {
		ctx.Mux = "tmux"
		ctx.TmuxSession = firstNonEmpty(os.Getenv("TATAMI_TMUX_SESSION"), os.Getenv("TMUX_SESSION"), tmuxDisplay("#S"))
		ctx.TmuxWindow = firstNonEmpty(os.Getenv("TATAMI_TMUX_WINDOW"), os.Getenv("TMUX_WINDOW"), tmuxDisplay("#I"))
		ctx.TmuxPane = firstNonEmpty(os.Getenv("TATAMI_TMUX_PANE"), os.Getenv("TMUX_PANE"), tmuxDisplay("#{pane_id}"))
	}
	return ctx
}

func tmuxDisplay(format string) string {
	out, err := exec.Command("tmux", "display-message", "-p", format).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Store) Path() string { return s.path }

func (s *Store) Create(session *Session) error {
	sessions, err := s.List()
	if err != nil {
		return err
	}
	if session.ID == "" {
		session.ID = NewSession(session.Agent, session.Args, session.Cwd, session.Context).ID
	}
	existing := map[string]bool{}
	for _, item := range sessions {
		existing[item.ID] = true
	}
	base := session.ID
	for i := 2; existing[session.ID]; i++ {
		session.ID = base + "-" + strconv.Itoa(i)
	}
	sessions = append(sessions, session)
	return s.save(sessions)
}

func (s *Store) Update(session *Session) error {
	sessions, err := s.List()
	if err != nil {
		return err
	}
	for i := range sessions {
		if sessions[i].ID == session.ID {
			sessions[i] = session
			return s.save(sessions)
		}
	}
	return fmt.Errorf("agent session %q not found", session.ID)
}

func (s *Store) Get(id string) (*Session, error) {
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ID == id || strings.HasPrefix(session.ID, id) {
			return session, nil
		}
	}
	return nil, fmt.Errorf("agent session %q not found", id)
}

func (s *Store) List() ([]*Session, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Session{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []*Session{}, nil
	}
	var sessions []*Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Store) PruneStale() (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, session := range sessions {
		if session.Status != StatusRunning {
			continue
		}
		if !processAlive(session.PID) {
			now := time.Now().UTC()
			session.Status = StatusStale
			session.EndedAt = &now
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	return changed, s.save(sessions)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func (s *Store) save(sessions []*Session) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0600)
}
