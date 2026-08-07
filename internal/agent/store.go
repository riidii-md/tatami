package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	StatusRunning = "running"
	StatusExited  = "exited"
	StatusStale   = "stale"
	StatusUnknown = "unknown"
)

const sessionFileSuffix = ".json"

var fallbackIDSuffix atomic.Uint64

// Context captures the terminal or multiplexer location where an AI CLI started.
type Context struct {
	Mux           string `json:"mux"`
	TTY           string `json:"tty,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	ZellijSession string `json:"zellij_session,omitempty"`
	ZellijPaneID  string `json:"zellij_pane_id,omitempty"`
	TmuxSession   string `json:"tmux_session,omitempty"`
	TmuxWindow    string `json:"tmux_window,omitempty"`
	TmuxPane      string `json:"tmux_pane,omitempty"`
	HerdrSession  string `json:"herdr_session,omitempty"`
	KittyWindowID string `json:"kitty_window_id,omitempty"`
}

// Session is one Tatami-tracked AI agent process. Command arguments are
// deliberately excluded because prompts and flags can contain secrets.
type Session struct {
	ID        string     `json:"id"`
	Agent     string     `json:"agent"`
	Cwd       string     `json:"cwd"`
	PID       int        `json:"pid"`
	Status    string     `json:"status"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Context   Context    `json:"context"`
}

// Store persists each tracked session in its own private JSON file. Per-session
// files prevent concurrent agent starts from overwriting one another.
type Store struct {
	dir            string
	isProcessAlive func(int) bool
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, isProcessAlive: processAlive}
}

func NewSession(agentName, cwd string, ctx Context) *Session {
	now := time.Now().UTC()
	id := fmt.Sprintf(
		"%s-%s-%s",
		newIDSuffix(),
		now.Format("20060102-150405.000000000"),
		sanitizeIDPart(agentName),
	)
	return &Session{
		ID:        id,
		Agent:     agentName,
		Cwd:       cwd,
		Status:    StatusRunning,
		StartedAt: now,
		Context:   ctx,
	}
}

func newIDSuffix() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err == nil {
		return hex.EncodeToString(random)
	}
	return fmt.Sprintf("%x-%s", os.Getpid(), strconv.FormatUint(fallbackIDSuffix.Add(1), 36))
}

func sanitizeIDPart(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
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
	host, _ := os.Hostname()
	ctx := Context{TTY: detectTTY(), Hostname: host, Mux: "none"}

	if os.Getenv("HERDR_ENV") == "1" {
		ctx.Mux = "herdr"
		ctx.HerdrSession = os.Getenv("HERDR_SESSION")
		return ctx
	}
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
		return ctx
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		ctx.Mux = "kitty"
		ctx.KittyWindowID = os.Getenv("KITTY_WINDOW_ID")
	}
	return ctx
}

func detectTTY() string {
	for _, path := range []string{"/proc/self/fd/0", "/dev/fd/0"} {
		if target, err := os.Readlink(path); err == nil {
			return target
		}
	}
	return ""
}

func tmuxDisplay(format string) string {
	out, err := exec.Command("tmux", "display-message", "-p", format).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Store) Path() string { return s.dir }

func (s *Store) Create(session *Session) error {
	if session == nil {
		return errors.New("agent session is required")
	}
	if session.ID == "" {
		session.ID = NewSession(session.Agent, session.Cwd, session.Context).ID
	}
	return s.withLock(func() error {
		path, err := s.sessionPath(session.ID)
		if err != nil {
			return err
		}
		data, err := marshalSession(session)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("agent session %q already exists", session.ID)
			}
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return err
		}
		return nil
	})
}

func (s *Store) Update(session *Session) error {
	if session == nil {
		return errors.New("agent session is required")
	}
	return s.withLock(func() error {
		return s.updateUnlocked(session)
	})
}

func (s *Store) updateUnlocked(session *Session) error {
	path, err := s.sessionPath(session.ID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("agent session %q not found", session.ID)
		}
		return err
	}
	data, err := marshalSession(session)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".session-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) Get(id string) (*Session, error) {
	if id == "" {
		return nil, errors.New("agent session id is required")
	}
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	var matches []*Session
	for _, session := range sessions {
		if session.ID == id {
			return session, nil
		}
		if strings.HasPrefix(session.ID, id) {
			matches = append(matches, session)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("agent session %q not found", id)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("agent session id %q is ambiguous", id)
	}
}

func (s *Store) List() ([]*Session, error) {
	var sessions []*Session
	err := s.withLock(func() error {
		var err error
		sessions, err = s.listUnlocked()
		return err
	})
	return sessions, err
}

func (s *Store) listUnlocked() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*Session{}, nil
		}
		return nil, err
	}
	sessions := make([]*Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), sessionFileSuffix) {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if session.ID+sessionFileSuffix != entry.Name() {
			return nil, fmt.Errorf("session id %q does not match file %s", session.ID, path)
		}
		sessions = append(sessions, &session)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].StartedAt.Equal(sessions[j].StartedAt) {
			return sessions[i].ID > sessions[j].ID
		}
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Store) PruneStale() (int, error) {
	changed := 0
	err := s.withLock(func() error {
		sessions, err := s.listUnlocked()
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if session.Status != StatusRunning || s.isProcessAlive(session.PID) {
				continue
			}
			now := time.Now().UTC()
			session.Status = StatusStale
			session.EndedAt = &now
			if err := s.updateUnlocked(session); err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	return changed, err
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (s *Store) ensureDir() error {
	if s.dir == "" {
		return errors.New("agent store directory is required")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return os.Chmod(s.dir, 0700)
}

func (s *Store) withLock(operation func() error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lock.Chmod(0600); err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return operation()
}

func (s *Store) sessionPath(id string) (string, error) {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", fmt.Errorf("invalid agent session id %q", id)
	}
	return filepath.Join(s.dir, id+sessionFileSuffix), nil
}

func marshalSession(session *Session) ([]byte, error) {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
