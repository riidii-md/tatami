package herdrhub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTripAndLocal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts.json")
	s := NewStore(p)
	if err := s.Save([]Endpoint{{ID: "work", Label: "Work", Target: "work"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Endpoint{LocalEndpoint(), {ID: "work", Label: "Work", Kind: EndpointSSH, Target: "work"}}) {
		t.Fatalf("List()=%#v", got)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("host config mode=%v", info.Mode())
	}
	if err := s.Save([]Endpoint{{ID: "work", Label: "Work edited", Target: "work"}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil || len(got) != 2 || got[1].Label != "Work edited" {
		t.Fatalf("updated List()=%#v err=%v", got, err)
	}
	if err := s.Delete("work"); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil || !reflect.DeepEqual(got, []Endpoint{LocalEndpoint()}) {
		t.Fatalf("after delete List()=%#v err=%v", got, err)
	}
	if err := s.Delete(LocalEndpointID); err == nil {
		t.Fatal("deleted local")
	}
	if err := s.Save([]Endpoint{{ID: "same", Label: "One", Target: "one"}, {ID: "same", Label: "Two", Target: "two"}}); err == nil {
		t.Fatal("duplicate endpoint IDs accepted")
	}
}

func TestRefreshKeepsLastSuccessfulSnapshot(t *testing.T) {
	client := NewClient(&fakeExec{err: errors.New("connection refused")})
	previous := Cache{Snapshots: []Snapshot{{EndpointID: "work", Sessions: []Session{{SessionKey: SessionKey{EndpointID: "work", SessionName: "same"}}}, LastSuccess: time.Now()}}}
	got := Refresh(context.Background(), client, []Endpoint{{ID: "work", Label: "Work", Target: "work"}}, previous, 1)
	if len(got) != 1 || got[0].State != StateStale || len(got[0].Sessions) != 1 {
		t.Fatalf("Refresh() = %#v", got)
	}
}
func TestStoreRejectsUnsafeTargetAndKeepsCorruption(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts.json")
	for _, v := range []string{"-oProxyCommand=x", "a b", "@host", "user@", "user@@host", "host\nnext", "host;touch", "host$HOME"} {
		if err := ValidateEndpoint(Endpoint{ID: "x", Label: "X", Target: v}); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	if err := os.WriteFile(p, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(p).List(); err == nil {
		t.Fatal("corruption accepted")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{" {
		t.Fatal("corrupt config changed")
	}
}

func TestEndpointAcceptsSafeDirectSSHDestinations(t *testing.T) {
	for _, target := range []string{
		"macmini",
		"100.108.232.8",
		"bmo.local",
		"oles@100.108.232.8",
		"oles@bmo.local",
		"ssh://oles@bmo.local:2222",
		"ssh://oles@[fd00::1]:2222",
	} {
		if err := ValidateEndpoint(Endpoint{ID: "macmini", Label: "Mac Mini", Target: target}); err != nil {
			t.Errorf("ValidateEndpoint target %q: %v", target, err)
		}
	}
}

func TestEndpointRejectsUnsafeSSHURIDestinations(t *testing.T) {
	for _, target := range []string{
		"http://oles@bmo.local",
		"ssh://oles:secret@bmo.local",
		"ssh://oles@bmo.local/path",
		"ssh://oles@bmo.local:invalid",
		"ssh://oles@bmo.local:70000",
	} {
		if err := ValidateEndpoint(Endpoint{ID: "macmini", Label: "Mac Mini", Target: target}); err == nil {
			t.Errorf("ValidateEndpoint accepted unsafe target %q", target)
		}
	}
}

func TestRejectsUnsafeRemoteInventoryFields(t *testing.T) {
	remote := Endpoint{ID: "work", Label: "Work", Target: "work"}
	if _, _, err := AttachArgs(remote, "safe;touch"); err == nil {
		t.Fatal("unsafe remote attach argument accepted")
	}
	if _, _, err := AgentArgs(remote, "safe;touch"); err == nil {
		t.Fatal("unsafe remote session command argument accepted")
	}
	if _, err := ParseSessions("work", []byte("{\"sessions\":[{\"name\":\"bad\\u001b[2J\"}]}")); err == nil {
		t.Fatal("terminal control sequence accepted in remote session")
	}
	if _, err := ParseAgents([]byte("{\"result\":{\"agents\":[{\"agent\":\"codex\\u001b[2J\",\"agent_status\":\"working\",\"cwd\":\"/repo\",\"pane_id\":\"p\"}]}}")); err == nil {
		t.Fatal("terminal control sequence accepted in remote agent")
	}
}
func TestExactQueryAndAttachArgs(t *testing.T) {
	remote := Endpoint{ID: "work", Label: "Work", Target: "oles@bmo.local"}
	n, a, err := QueryArgs(remote)
	if err != nil || n != "ssh" || !reflect.DeepEqual(a, []string{"-o", "BatchMode=yes", "--", "oles@bmo.local", "herdr", "session", "list", "--json"}) {
		t.Fatalf("query %s %#v %v", n, a, err)
	}
	n, a, err = AttachArgs(remote, "same")
	if err != nil || n != "herdr" || !reflect.DeepEqual(a, []string{"--remote", "oles@bmo.local", "--session", "same"}) {
		t.Fatalf("attach %s %#v %v", n, a, err)
	}
	n, a, err = AgentArgs(remote, "same")
	if err != nil || n != "ssh" || !reflect.DeepEqual(a, []string{"-o", "BatchMode=yes", "--", "oles@bmo.local", "herdr", "--session", "same", "agent", "list"}) {
		t.Fatalf("agents %s %#v %v", n, a, err)
	}
}
func TestParseAgentsAllowsOnlySafeFields(t *testing.T) {
	agents, err := ParseAgents([]byte(`{"result":{"agents":[{"agent":"codex","agent_status":"working","cwd":"/safe","pane_id":"p","terminal":"SECRET"}]}}`))
	if err != nil || !reflect.DeepEqual(agents, []Agent{{Kind: "codex", Status: "working", CWD: "/safe"}}) {
		t.Fatalf("agents=%#v err=%v", agents, err)
	}
}

type fakeExec struct {
	name   string
	args   []string
	out    []byte
	err    error
	stderr []byte
}

func (f *fakeExec) Output(_ context.Context, n string, a ...string) (ExecResult, error) {
	f.name = n
	f.args = append([]string(nil), a...)
	return ExecResult{Stdout: f.out, Stderr: f.stderr}, f.err
}

type blockingExec struct{}

func (blockingExec) Output(ctx context.Context, _ string, _ ...string) (ExecResult, error) {
	<-ctx.Done()
	return ExecResult{}, ctx.Err()
}

func TestClientEndpointLocalErrors(t *testing.T) {
	f := &fakeExec{err: errors.New("exit status 255"), stderr: []byte("Permission denied (publickey)")}
	s := NewClient(f).Query(context.Background(), Endpoint{ID: "work", Label: "Work", Target: "work"})
	if s.State != StateAuthenticationNeeded {
		t.Fatalf("state=%s", s.State)
	}
}
func TestClientOnlineInvalidJSONAndTimeoutStates(t *testing.T) {
	endpoint := Endpoint{ID: "work", Label: "Work", Target: "work"}
	online := NewClient(&fakeExec{out: []byte(`{"sessions":[{"name":"same","running":true}]}`)}).Query(context.Background(), endpoint)
	if online.State != StateOnline || len(online.Sessions) != 1 || online.Sessions[0].EndpointID != "work" {
		t.Fatalf("online snapshot = %#v", online)
	}
	invalid := NewClient(&fakeExec{out: []byte(`{`)}).Query(context.Background(), endpoint)
	if invalid.State != StateIncompatible {
		t.Fatalf("invalid JSON state = %s", invalid.State)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	timedOut := NewClient(blockingExec{}).Query(ctx, endpoint)
	if timedOut.State != StateOffline {
		t.Fatalf("timeout state = %s", timedOut.State)
	}
}
func TestClientClassifiesMissingHerdrFromStderr(t *testing.T) {
	s := NewClient(&fakeExec{err: errors.New("exit status 127"), stderr: []byte("herdr: command not found")}).Query(context.Background(), Endpoint{ID: "work", Label: "Work", Target: "work"})
	if s.State != StateIncompatible {
		t.Fatalf("state=%s", s.State)
	}
}
func TestCacheIsVersionedAndDoesNotPersistErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	if err := SaveCache(p, Cache{Snapshots: []Snapshot{{EndpointID: "work", State: StateOffline, Error: "secret stderr"}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), `"version":1`) {
		t.Fatalf("unsafe cache: %s", b)
	}
	if err := os.WriteFile(p, []byte(`{"version":2,"snapshots":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(p); err == nil {
		t.Fatal("unsupported cache accepted")
	}
}

func TestCacheRejectsUnsafeTerminalData(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	data := `{"version":1,"snapshots":[{"endpoint_id":"work","state":"online","sessions":[{"endpoint_id":"work","session_name":"bad\u001b[2J"}]}]}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(p); err == nil {
		t.Fatal("unsafe cached terminal data accepted")
	}
}
func TestCachePrivateMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	if err := SaveCache(p, Cache{}); err != nil {
		t.Fatal(err)
	}
	i, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if i.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v", i.Mode())
	}
}

func TestBoundedOutputCapsUntrustedCommandData(t *testing.T) {
	w := &boundedOutput{limit: 4}
	if n, err := w.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if string(w.data) != "abcd" || !w.truncated {
		t.Fatalf("bounded output = %q, truncated=%v", w.data, w.truncated)
	}
}
