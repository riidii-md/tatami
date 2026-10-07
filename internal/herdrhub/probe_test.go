package herdrhub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type probeStatus int

func (s probeStatus) Error() string { return "exit status" }
func (s probeStatus) ExitCode() int { return int(s) }

func TestRemoteProbeOneConnection(t *testing.T) {
	inv := "TATAMI-HUB-PROBE 1 inventory\n" + `{"kind":"tatami.hub.inventory","version":1,"host":"box","workspaces":[],"sessions":[],"hosts":[]}`
	for _, tc := range []struct {
		name, output string
		err          error
		kind         FailureKind
	}{
		{"inventory", inv, nil, ""},
		{"legacy", "TATAMI-HUB-PROBE 1 sessions\n" + `{"sessions":[]}`, nil, ""},
		{"missing", "TATAMI-HUB-PROBE 1 missing\n", probeStatus(127), FailureMissingTools},
		{"missingIllegalBody", "TATAMI-HUB-PROBE 1 missing\n{}", probeStatus(127), FailureIncompatible},
		{"selected127", "TATAMI-HUB-PROBE 1 inventory\n", probeStatus(127), FailureSelectedTool},
		{"selected1", "TATAMI-HUB-PROBE 1 sessions\n", probeStatus(1), FailureSelectedTool},
		{"selected255", "TATAMI-HUB-PROBE 1 inventory\n", probeStatus(255), FailureIndeterminate},
		{"transport255", "", probeStatus(255), FailureIndeterminate},
		{"noise", "banner\n" + inv, nil, FailureIncompatible},
		{"badjson", "TATAMI-HUB-PROBE 1 sessions\n{}", nil, FailureIncompatible},
		{"unknownversion", "TATAMI-HUB-PROBE 2 sessions\n{}", nil, FailureIncompatible},
		{"invalidOverride", "TATAMI-HUB-PROBE 1 missing\n", probeStatus(126), FailureInvalidOverride},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &queuedExec{results: []ExecResult{{Stdout: []byte(tc.output)}}, errors: []error{tc.err}}
			snap := NewClient(f).Query(context.Background(), Endpoint{ID: "box", Label: "Box", Target: "box"})
			if len(f.calls) != 1 {
				t.Fatalf("SSH calls=%d", len(f.calls))
			}
			if snap.Failure != tc.kind {
				t.Fatalf("failure=%s want=%s snapshot=%+v", snap.Failure, tc.kind, snap)
			}
			if tc.kind == "" && snap.State != StateOnline {
				t.Fatal(snap)
			}
		})
	}
}
func TestProbeTruncationPrecedesSelectedFailure(t *testing.T) {
	_, err := parseProbe(context.Background(), Endpoint{ID: "box"}, ExecResult{Stdout: []byte("TATAMI-HUB-PROBE 1 inventory\n"), StdoutTruncated: true}, probeStatus(1))
	var q *QueryError
	if !errors.As(err, &q) || q.Kind != FailureOutputLimit {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = parseProbe(ctx, Endpoint{}, ExecResult{StderrTruncated: true}, probeStatus(255))
	if !errors.As(err, &q) || q.Kind != FailureCancelled {
		t.Fatalf("%v", err)
	}
}
func TestStrictLegacySessions(t *testing.T) {
	for _, input := range []string{"null", "[]", "{}", `{"sessions":null}`, `{"sessions":{}}`, `{"sessions":[{}]}`, `{"sessions":[{"name":" "} ]}`} {
		if _, err := ParseSessions("box", []byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	sessions := make([]map[string]string, 512)
	for i := range sessions {
		sessions[i] = map[string]string{"name": "okay"}
	}
	for _, count := range []int{512, 513} {
		if count == 513 {
			sessions = append(sessions, map[string]string{"name": "extra"})
		}
		b, _ := json.Marshal(map[string]any{"sessions": sessions})
		_, err := ParseSessions("box", b)
		if (err != nil) != (count > 512) {
			t.Fatalf("count=%d err=%v", count, err)
		}
	}
	if _, err := ParseSessions("box", []byte(`{"sessions":[],"extra":true}`)); err != nil {
		t.Fatal(err)
	}
}
func TestProbeBootstrapFindsUserInstallAndNeverFallsBackSelectedFailure(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tatami := filepath.Join(dir, "tatami")
	herdr := filepath.Join(dir, "herdr")
	write := func(path, script string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write(herdr, "printf '%s\\n' '"+`{"sessions":[]}`+"'")
	run := func() ([]byte, error) {
		script := strings.NewReplacer("/usr/local/bin", home+"/empty-local", "/opt/homebrew/bin", home+"/empty-homebrew").Replace(probeScript)
		cmd := exec.Command("/bin/sh", "-c", script, "probe", "", "")
		cmd.Env = []string{"HOME=" + home, "PATH=" + home + "/empty-path"}
		return cmd.CombinedOutput()
	}
	out, err := run()
	if err != nil || !strings.HasPrefix(string(out), "TATAMI-HUB-PROBE 1 sessions\n") {
		t.Fatalf("%v %s", err, out)
	}
	write(tatami, "exit 127")
	out, err = run()
	if err == nil || string(out) != "TATAMI-HUB-PROBE 1 inventory\n" {
		t.Fatalf("selected fallback: %v %s", err, out)
	}
	if err := os.Remove(tatami); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(herdr); err != nil {
		t.Fatal(err)
	}
	out, err = run()
	if err == nil || string(out) != "TATAMI-HUB-PROBE 1 missing\n" {
		t.Fatalf("%v %s", err, out)
	}
}

func TestResolverSearchLocationsAndOverridePrecedence(t *testing.T) {
	for _, location := range []string{"path", "go/bin", ".local/bin", "usr-local", "homebrew"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			installed := filepath.Join(root, location)
			if err := os.MkdirAll(installed, 0700); err != nil {
				t.Fatal(err)
			}
			fixture := []byte("#!/bin/sh\nprintf '%s\\n' '{\"kind\":\"tatami.hub.inventory\",\"version\":1,\"host\":\"fixture\",\"workspaces\":[],\"sessions\":[],\"hosts\":[]}'\n")
			if err := os.WriteFile(filepath.Join(installed, "tatami"), fixture, 0700); err != nil {
				t.Fatal(err)
			}
			script := strings.NewReplacer("/usr/local/bin", root+"/usr-local", "/opt/homebrew/bin", root+"/homebrew").Replace(probeScript)
			run := func(override string) ([]byte, error) {
				cmd := exec.Command("/bin/sh", "-c", script, "probe", override, "")
				cmd.Env = []string{"HOME=" + root, "PATH=" + root + "/path"}
				return cmd.CombinedOutput()
			}
			out, err := run("")
			if err != nil || !strings.HasPrefix(string(out), "TATAMI-HUB-PROBE 1 inventory\n") {
				t.Fatalf("location %s: %v %s", location, err, out)
			}
			out, err = run(root + "/absent-override")
			var status *exec.ExitError
			if !errors.As(err, &status) || status.ExitCode() != 126 || string(out) != "TATAMI-HUB-PROBE 1 missing\n" {
				t.Fatalf("invalid explicit override fell through: %v %s", err, out)
			}
			out, err = run(filepath.Join(installed, "tatami"))
			if err != nil || !strings.HasPrefix(string(out), "TATAMI-HUB-PROBE 1 inventory\n") {
				t.Fatalf("valid override %v %s", err, out)
			}
		})
	}
}
