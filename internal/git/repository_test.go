package git

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSanitizeRepositoryIdentity(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https credentials", raw: "https://user:secret@GitHub.com/riidii/tatami.git?token=secret#frag", want: "github.com/riidii/tatami"},
		{name: "ssh URL", raw: "ssh://git@github.com:22/riidii/tatami.git", want: "github.com/riidii/tatami"},
		{name: "scp", raw: "git@github.com:riidii/tatami.git", want: "github.com/riidii/tatami"},
		{name: "trailing slash", raw: "https://github.com/riidii/tatami.git/", want: "github.com/riidii/tatami"},
		{name: "display form", raw: "github.com/riidii/tatami", want: "github.com/riidii/tatami"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := SanitizeRepositoryIdentity(test.raw)
			if err != nil || got != test.want {
				t.Fatalf("SanitizeRepositoryIdentity() = %q, %v; want %q", got, err, test.want)
			}
			if strings.Contains(got, "secret") || strings.Contains(got, "git@") {
				t.Fatalf("unsafe identity %q", got)
			}
		})
	}
}

func TestSanitizeRepositoryIdentityRejectsUnsafeValuesWithoutEcho(t *testing.T) {
	secret := "credential-sentinel"
	values := []string{
		"file:///tmp/org/repo",
		"file:/tmp/org/repo",
		"local:/tmp/org/repo",
		"/tmp/org/repo",
		"../org/repo",
		"https://github.com/one",
		"https://git%40github.com/org/repo",
		"https%3A%2F%2Fuser%3Asecret%40github.com/org/repo",
		"https://github.com/org/repo%2540credential-sentinel",
		"https://github.com/org/repo\n" + secret,
		"https://github.com/org/../repo",
		"https://github.com/org/" + strings.Repeat("r", MaxRepositoryDisplayBytes),
		strings.Repeat("x", MaxRepositoryOriginBytes+1),
	}
	for _, value := range values {
		got, err := SanitizeRepositoryIdentity(value)
		if err == nil || got != "" {
			t.Fatalf("SanitizeRepositoryIdentity(%q) = %q, %v", value, got, err)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), value) {
			t.Fatalf("error leaked input: %v", err)
		}
	}
}

type fakeRepositoryCommand struct {
	outputs map[string][]byte
	calls   [][]string
	err     error
}

func (f *fakeRepositoryCommand) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.outputs[args[len(args)-1]]...), nil
}

func TestRepositoryResolverCachesByCommonDirectory(t *testing.T) {
	executor := &fakeRepositoryCommand{outputs: map[string][]byte{
		"--git-common-dir":  []byte("/repo/.git\n"),
		"remote.origin.url": []byte("https://user:secret@github.com/riidii/tatami.git\n"),
	}}
	resolver := NewRepositoryResolver(2)
	resolver.executor = executor
	first, err := resolver.Resolve(context.Background(), "/repo/main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), "/repo/worktree")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Display != "github.com/riidii/tatami" {
		t.Fatalf("identities = %#v %#v", first, second)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("calls = %#v; want common, origin, common", executor.calls)
	}
}

func TestRepositoryResolverReturnsGenericErrors(t *testing.T) {
	executor := &fakeRepositoryCommand{err: errors.New("credential-sentinel")}
	resolver := NewRepositoryResolver(1)
	resolver.executor = executor
	_, err := resolver.Resolve(context.Background(), "/repo")
	if !errors.Is(err, ErrRepositoryIdentityUnavailable) || strings.Contains(err.Error(), "credential-sentinel") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

type coalescingRepositoryCommand struct {
	mu            sync.Mutex
	originStarted chan struct{}
	releaseOrigin chan struct{}
	originCalls   int
	once          sync.Once
}

func (f *coalescingRepositoryCommand) Output(ctx context.Context, _ string, args ...string) ([]byte, error) {
	if args[len(args)-1] == "--git-common-dir" {
		return []byte("/repo/.git\n"), nil
	}
	f.mu.Lock()
	f.originCalls++
	f.mu.Unlock()
	f.once.Do(func() { close(f.originStarted) })
	select {
	case <-f.releaseOrigin:
		return []byte("https://github.com/riidii/tatami.git\n"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRepositoryResolverCoalescesConcurrentOriginLookup(t *testing.T) {
	executor := &coalescingRepositoryCommand{originStarted: make(chan struct{}), releaseOrigin: make(chan struct{})}
	resolver := NewRepositoryResolver(4)
	resolver.executor = executor
	results := make(chan error, 2)
	go func() { _, err := resolver.Resolve(context.Background(), "/repo/main"); results <- err }()
	<-executor.originStarted
	go func() { _, err := resolver.Resolve(context.Background(), "/repo/worktree"); results <- err }()
	time.Sleep(20 * time.Millisecond)
	close(executor.releaseOrigin)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	executor.mu.Lock()
	originCalls := executor.originCalls
	executor.mu.Unlock()
	if originCalls != 1 {
		t.Fatalf("origin calls = %d, want 1", originCalls)
	}
}

type boundedRepositoryCommand struct {
	active    atomic.Int32
	maxActive atomic.Int32
	block     bool
	oversize  bool
}

func (f *boundedRepositoryCommand) Output(ctx context.Context, _ string, args ...string) ([]byte, error) {
	active := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		maximum := f.maxActive.Load()
		if active <= maximum || f.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	time.Sleep(10 * time.Millisecond)
	if args[len(args)-1] == "--git-common-dir" {
		return []byte(args[1] + "/.git\n"), nil
	}
	if f.oversize {
		return []byte(strings.Repeat("x", MaxRepositoryOriginBytes+1)), nil
	}
	return []byte("https://github.com/riidii/tatami.git\n"), nil
}

func TestRepositoryResolverBoundsConcurrencyAndTimeout(t *testing.T) {
	executor := &boundedRepositoryCommand{}
	resolver := NewRepositoryResolver(2)
	resolver.executor = executor
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = resolver.Resolve(context.Background(), "/repo/"+string(rune('a'+i)))
		}()
	}
	wg.Wait()
	if got := executor.maxActive.Load(); got > 2 {
		t.Fatalf("maximum concurrent commands = %d, want <= 2", got)
	}

	blocking := &boundedRepositoryCommand{block: true}
	resolver = NewRepositoryResolver(1)
	resolver.executor = blocking
	started := time.Now()
	_, err := resolver.Resolve(context.Background(), "/repo/timeout")
	if !errors.Is(err, ErrRepositoryIdentityUnavailable) || time.Since(started) > time.Second {
		t.Fatalf("timeout error=%v elapsed=%v", err, time.Since(started))
	}
}

func TestRepositoryResolverRejectsOversizedCommandOutput(t *testing.T) {
	executor := &boundedRepositoryCommand{oversize: true}
	resolver := NewRepositoryResolver(1)
	resolver.executor = executor
	_, err := resolver.Resolve(context.Background(), "/repo")
	if !errors.Is(err, ErrRepositoryIdentityUnavailable) {
		t.Fatalf("oversized output error = %v", err)
	}
}
