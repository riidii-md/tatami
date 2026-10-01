package git

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	MaxRepositoryOriginBytes  = 8 * 1024
	MaxRepositoryDisplayBytes = 1024
	repositoryCommandTimeout  = 250 * time.Millisecond
)

var ErrRepositoryIdentityUnavailable = errors.New("repository identity unavailable")

type RepositoryIdentity struct {
	CommonDir string
	Display   string
}

type repositoryCommandExecutor interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execRepositoryCommand struct{}

func (execRepositoryCommand) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var output limitedBuffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, err
	}
	if output.overflow {
		return nil, ErrRepositoryIdentityUnavailable
	}
	return output.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := MaxRepositoryOriginBytes + 1 - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(value)
	return original, nil
}

type RepositoryResolver struct {
	executor  repositoryCommandExecutor
	semaphore chan struct{}
	mu        sync.Mutex
	cache     map[string]RepositoryIdentity
	inFlight  map[string]*repositoryFlight
}

type repositoryFlight struct {
	done     chan struct{}
	identity RepositoryIdentity
	err      error
}

func NewRepositoryResolver(maxConcurrent int) *RepositoryResolver {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &RepositoryResolver{
		executor:  execRepositoryCommand{},
		semaphore: make(chan struct{}, maxConcurrent),
		cache:     make(map[string]RepositoryIdentity),
		inFlight:  make(map[string]*repositoryFlight),
	}
}

func (r *RepositoryResolver) Resolve(ctx context.Context, workspacePath string) (RepositoryIdentity, error) {
	if r == nil || r.executor == nil || strings.TrimSpace(workspacePath) == "" {
		return RepositoryIdentity{}, ErrRepositoryIdentityUnavailable
	}
	select {
	case r.semaphore <- struct{}{}:
		defer func() { <-r.semaphore }()
	case <-ctx.Done():
		return RepositoryIdentity{}, ErrRepositoryIdentityUnavailable
	}

	commonOutput, err := r.run(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return RepositoryIdentity{}, ErrRepositoryIdentityUnavailable
	}
	commonDir := strings.TrimSpace(string(commonOutput))
	if commonDir == "" || len(commonDir) > MaxRepositoryOriginBytes || containsControl(commonDir) {
		return RepositoryIdentity{}, ErrRepositoryIdentityUnavailable
	}
	commonDir = filepath.Clean(commonDir)

	r.mu.Lock()
	identity, ok := r.cache[commonDir]
	if ok {
		r.mu.Unlock()
		return identity, nil
	}
	if flight, exists := r.inFlight[commonDir]; exists {
		r.mu.Unlock()
		select {
		case <-flight.done:
			return flight.identity, flight.err
		case <-ctx.Done():
			return RepositoryIdentity{}, ErrRepositoryIdentityUnavailable
		}
	}
	flight := &repositoryFlight{done: make(chan struct{})}
	r.inFlight[commonDir] = flight
	r.mu.Unlock()

	originOutput, err := r.run(ctx, workspacePath, "config", "--get", "remote.origin.url")
	if err != nil || len(originOutput) > MaxRepositoryOriginBytes {
		return r.finishFlight(commonDir, flight, RepositoryIdentity{}, ErrRepositoryIdentityUnavailable)
	}
	display, err := SanitizeRepositoryIdentity(string(originOutput))
	if err != nil {
		return r.finishFlight(commonDir, flight, RepositoryIdentity{}, ErrRepositoryIdentityUnavailable)
	}
	identity = RepositoryIdentity{CommonDir: commonDir, Display: display}
	return r.finishFlight(commonDir, flight, identity, nil)
}

func (r *RepositoryResolver) finishFlight(commonDir string, flight *repositoryFlight, identity RepositoryIdentity, err error) (RepositoryIdentity, error) {
	r.mu.Lock()
	if err == nil {
		r.cache[commonDir] = identity
	}
	flight.identity = identity
	flight.err = err
	delete(r.inFlight, commonDir)
	close(flight.done)
	r.mu.Unlock()
	return identity, err
}

func (r *RepositoryResolver) run(parent context.Context, workspacePath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, repositoryCommandTimeout)
	defer cancel()
	gitArgs := append([]string{"-C", workspacePath}, args...)
	return r.executor.Output(ctx, "git", gitArgs...)
}

func SanitizeRepositoryIdentity(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > MaxRepositoryOriginBytes {
		return "", ErrRepositoryIdentityUnavailable
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || containsControl(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasPrefix(raw, "~/") {
		return "", ErrRepositoryIdentityUnavailable
	}

	var host, repositoryPath string
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http" && parsed.Scheme != "ssh") {
			return "", ErrRepositoryIdentityUnavailable
		}
		host = parsed.Hostname()
		repositoryPath = parsed.EscapedPath()
	} else if colon := strings.IndexByte(raw, ':'); colon > 0 && !strings.Contains(raw[:colon], "/") {
		left := raw[:colon]
		if at := strings.LastIndexByte(left, '@'); at >= 0 {
			left = left[at+1:]
		}
		if strings.EqualFold(left, "file") || strings.EqualFold(left, "local") {
			return "", ErrRepositoryIdentityUnavailable
		}
		host = left
		repositoryPath = raw[colon+1:]
	} else {
		slash := strings.IndexByte(raw, '/')
		if slash <= 0 {
			return "", ErrRepositoryIdentityUnavailable
		}
		host = raw[:slash]
		repositoryPath = raw[slash+1:]
	}

	host = strings.ToLower(strings.TrimSpace(host))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	if !validRepositoryHost(host) {
		return "", ErrRepositoryIdentityUnavailable
	}
	repositoryPath = strings.TrimSpace(strings.TrimPrefix(repositoryPath, "/"))
	if cut := strings.IndexAny(repositoryPath, "?#"); cut >= 0 {
		repositoryPath = repositoryPath[:cut]
	}
	if decoded, err := url.PathUnescape(repositoryPath); err == nil {
		repositoryPath = decoded
	} else {
		return "", ErrRepositoryIdentityUnavailable
	}
	repositoryPath = strings.Trim(repositoryPath, "/")
	repositoryPath = strings.TrimSuffix(repositoryPath, ".git")
	if !validRepositoryPath(repositoryPath) {
		return "", ErrRepositoryIdentityUnavailable
	}
	display := host + "/" + repositoryPath
	if len(display) > MaxRepositoryDisplayBytes {
		return "", ErrRepositoryIdentityUnavailable
	}
	return display, nil
}

func validRepositoryHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "@/?#\\") {
		return false
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return true
	}
	for _, r := range host {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-') {
			return false
		}
	}
	return !strings.HasPrefix(host, ".") && !strings.HasSuffix(host, ".")
}

func validRepositoryPath(repositoryPath string) bool {
	if repositoryPath == "" || containsControl(repositoryPath) || strings.ContainsAny(repositoryPath, "%:@?#\\") {
		return false
	}
	parts := strings.Split(repositoryPath, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "@") {
			return false
		}
	}
	return true
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
