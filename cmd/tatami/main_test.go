package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OleksandrBesan/tatami/internal/agent"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestNewTabProcessStartsShellInWorkspace(t *testing.T) {
	ws := &workspace.Workspace{Name: "project", Path: "/tmp/project"}

	process, err := newTabProcess(ws, "/bin/sh", exec.LookPath)
	if err != nil {
		t.Fatalf("newTabProcess: %v", err)
	}

	if process.path != "/bin/sh" {
		t.Fatalf("process path = %q; want /bin/sh", process.path)
	}
	if process.dir != ws.Path {
		t.Fatalf("process dir = %q; want %q", process.dir, ws.Path)
	}
	if !reflect.DeepEqual(process.args, []string{"/bin/sh"}) {
		t.Fatalf("process args = %#v; want shell argv", process.args)
	}
}

func TestNewTabProcessStartsRemoteSession(t *testing.T) {
	ws := &workspace.Workspace{
		Name: "server",
		Remote: &workspace.Remote{
			Host: "user@example.com",
			Key:  "/tmp/my key",
			Path: "/srv/project with quote's",
		},
	}
	lookPath := func(name string) (string, error) {
		if name != "ssh" {
			t.Fatalf("looked up %q; want ssh", name)
		}
		return "/usr/bin/ssh", nil
	}

	process, err := newTabProcess(ws, "/bin/sh", lookPath)
	if err != nil {
		t.Fatalf("newTabProcess: %v", err)
	}

	wantArgs := []string{
		"/usr/bin/ssh",
		"-i", "/tmp/my key",
		"-t",
		"--",
		"user@example.com",
		"cd '/srv/project with quote'\"'\"'s' && exec ${SHELL:-/bin/sh}",
	}
	if process.path != "/usr/bin/ssh" {
		t.Fatalf("process path = %q; want /usr/bin/ssh", process.path)
	}
	if process.dir != "" {
		t.Fatalf("remote process dir = %q; want empty", process.dir)
	}
	if !reflect.DeepEqual(process.args, wantArgs) {
		t.Fatalf("process args = %#v; want %#v", process.args, wantArgs)
	}
}

func TestNewTabProcessStartsRemoteSessionThroughJumpRoute(t *testing.T) {
	ws := &workspace.Workspace{
		Name: "server",
		Remote: &workspace.Remote{
			Host: "macmini.internal",
			Jump: []string{"user@bastion", "relay"},
			Path: "/srv/project",
		},
	}
	process, err := newTabProcess(ws, "/bin/sh", func(string) (string, error) { return "/usr/bin/ssh", nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/usr/bin/ssh",
		"-J", "user@bastion,relay",
		"-t", "--", "macmini.internal",
		"cd '/srv/project' && exec ${SHELL:-/bin/sh}",
	}
	if !reflect.DeepEqual(process.args, want) {
		t.Fatalf("jump process args = %#v; want %#v", process.args, want)
	}
}

func TestNewTabTargetUsesSelectedWorktree(t *testing.T) {
	ws := &workspace.Workspace{Name: "project", Path: "/tmp/project"}
	result := &tui.Result{
		Action:    tui.ActionWorktree,
		Workspace: ws,
		Worktree:  &git.Worktree{Path: "/tmp/project-feature", Branch: "feature"},
		Template:  &workspace.Template{},
	}

	target, ok := newTabTarget(result)
	if !ok {
		t.Fatal("plain worktree result was not recognized as a new-tab target")
	}
	if target.Path != "/tmp/project-feature" || target.Name != "feature" {
		t.Fatalf("new-tab target = %#v; want selected worktree path and branch", target)
	}
	if ws.Path != "/tmp/project" {
		t.Fatalf("newTabTarget mutated source workspace path to %q", ws.Path)
	}
}

func TestHerdrTargetUsesSelectedWorktreeAndSavedLayout(t *testing.T) {
	ws := &workspace.Workspace{
		Name: "project",
		Path: "/tmp/project",
		Layout: workspace.Layout{
			Type:    workspace.LayoutHerdr,
			MainCmd: "claude",
		},
	}
	result := &tui.Result{
		Action:    tui.ActionWorktree,
		Workspace: ws,
		Worktree:  &git.Worktree{Path: "/tmp/project-feature", Branch: "feature"},
		Template:  &workspace.Template{},
	}

	target, ok := herdrTarget(result)
	if !ok {
		t.Fatal("Herdr worktree result was not recognized")
	}
	if target.Path != "/tmp/project-feature" || target.Name != "feature" {
		t.Fatalf("Herdr target = %#v; want selected worktree path and branch", target)
	}
	if target.Layout.Type != workspace.LayoutHerdr || target.Layout.MainCmd != "claude" {
		t.Fatalf("Herdr target layout = %#v; want saved Herdr layout", target.Layout)
	}
}

func TestHerdrSessionNameUsesDedicatedOrExplicitExistingMode(t *testing.T) {
	parent := &workspace.Workspace{Name: "project", Path: "/tmp/project", Layout: workspace.Layout{Type: workspace.LayoutHerdr}}
	target := &workspace.Workspace{Name: "feature", Path: "/tmp/project-feature", Layout: workspace.Layout{Type: workspace.LayoutHerdr}}

	dedicated := &tui.Result{Workspace: parent, HerdrMode: tui.HerdrOpenDedicated, HerdrSessionName: "feature-review"}
	if got := herdrSessionName(dedicated, target); got != "feature-review" {
		t.Fatalf("dedicated session = %q; want feature-review", got)
	}

	existing := &tui.Result{Workspace: parent, HerdrMode: tui.HerdrOpenExisting, HerdrSessionName: "team-session"}
	if got := herdrSessionName(existing, target); got != "team-session" {
		t.Fatalf("existing session = %q; want team-session", got)
	}
}

func TestHerdrDedicatedModeFallsBackToGeneratedSessionName(t *testing.T) {
	target := &workspace.Workspace{Name: "feature", Layout: workspace.Layout{Type: workspace.LayoutHerdr}}
	result := &tui.Result{HerdrMode: tui.HerdrOpenDedicated}

	if got := herdrSessionName(result, target); got != "tatami-feature" {
		t.Fatalf("generated dedicated session = %q; want tatami-feature", got)
	}
}

func TestHerdrExistingModeRequiresSelectedSession(t *testing.T) {
	parent := &workspace.Workspace{Name: "project", Layout: workspace.Layout{Type: workspace.LayoutHerdr}}
	target := &workspace.Workspace{Name: "feature", Layout: workspace.Layout{Type: workspace.LayoutHerdr}}
	result := &tui.Result{Workspace: parent, HerdrMode: tui.HerdrOpenExisting}

	if got := herdrSessionName(result, target); got != "" {
		t.Fatalf("existing session without selection = %q; want empty", got)
	}
}

func TestHerdrTargetUsesSelectedTemplate(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "project",
		Path:   "/tmp/project",
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	}
	template := &workspace.Template{
		Name:    "agents",
		MainCmd: "nvim",
		Panes:   []workspace.Pane{{Command: "claude", Direction: "right"}},
	}
	result := &tui.Result{
		Action:    tui.ActionWithTemplate,
		Workspace: ws,
		Template:  template,
	}

	target, ok := herdrTarget(result)
	if !ok {
		t.Fatal("Herdr template result was not recognized")
	}
	if target.Layout.Type != workspace.LayoutHerdr || target.Layout.MainCmd != "nvim" {
		t.Fatalf("Herdr template layout = %#v; want selected template on Herdr", target.Layout)
	}
	if !reflect.DeepEqual(target.Layout.Panes, template.Panes) {
		t.Fatalf("Herdr template panes = %#v; want %#v", target.Layout.Panes, template.Panes)
	}
}

func TestKittyConfigLaunchesNewTabMode(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/kitty-integration.sh", "--print")
	cmd.Env = append(os.Environ(), "TATAMI_BIN=/tmp/tatami", "SHELL=/bin/zsh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("print Kitty config: %v\n%s", err, output)
	}

	for _, binding := range []string{
		`map kitty_mod+t launch --type=tab --cwd=current /bin/zsh -lic "exec '/tmp/tatami' --new-tab"`,
		`map cmd+t launch --type=tab --cwd=current /bin/zsh -lic "exec '/tmp/tatami' --new-tab"`,
	} {
		if !strings.Contains(string(output), binding) {
			t.Errorf("Kitty config missing %q:\n%s", binding, output)
		}
	}
}

func TestParseLaunchOptionsEnablesMobileAndNewTabModes(t *testing.T) {
	options := parseLaunchOptions([]string{"--new-tab", "--mobile"})
	if !options.newTabMode {
		t.Fatal("--new-tab did not enable new-tab mode")
	}
	if !options.mobileMode {
		t.Fatal("--mobile did not enable mobile mode")
	}
	if options.showVersion {
		t.Fatal("ordinary launch options unexpectedly enabled version output")
	}

	versionOptions := parseLaunchOptions([]string{"-v"})
	if !versionOptions.showVersion {
		t.Fatal("-v did not enable version output")
	}

	shortMobileOptions := parseLaunchOptions([]string{"-m"})
	if !shortMobileOptions.mobileMode {
		t.Fatal("-m did not enable mobile mode")
	}
}

func TestLoadHerdrHubRejectsCorruptHostsWithoutChangingThem(t *testing.T) {
	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts.json")
	cache := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(hosts, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := loadHerdrHub(&config.Paths{HerdrHostsFile: hosts, HerdrHubFile: cache})
	if err == nil || !strings.Contains(err.Error(), "not modified") {
		t.Fatalf("load error = %v", err)
	}
	got, readErr := os.ReadFile(hosts)
	if readErr != nil || string(got) != "{" {
		t.Fatalf("corrupt hosts changed: %q, %v", got, readErr)
	}
}

func TestLoadHerdrHubPreservesCorruptCache(t *testing.T) {
	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts.json")
	cache := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(cache, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	endpoints, snapshot, writable, err := loadHerdrHub(&config.Paths{HerdrHostsFile: hosts, HerdrHubFile: cache})
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || endpoints[0].ID != herdrhub.LocalEndpointID || len(snapshot.Snapshots) != 0 || writable {
		t.Fatalf("load result endpoints=%#v cache=%#v writable=%v", endpoints, snapshot, writable)
	}
	got, readErr := os.ReadFile(cache)
	if readErr != nil || string(got) != "not-json" {
		t.Fatalf("corrupt cache changed: %q, %v", got, readErr)
	}
}

func TestHandleResultRejectsUnsafeRemoteSession(t *testing.T) {
	err := handleResult(&tui.Result{
		Action:          tui.ActionAttachHerdrSession,
		SessionName:     "safe;touch",
		HerdrEndpointID: "work",
		HerdrTarget:     "work",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("unsafe remote session error = %v", err)
	}
}

func TestHandleResultRejectsUnsafeRemoteEndpointAuthentication(t *testing.T) {
	err := handleResult(&tui.Result{
		Action:          tui.ActionAttachHerdrEndpoint,
		HerdrEndpointID: "work",
		HerdrTarget:     "host;touch",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "SSH destination") {
		t.Fatalf("unsafe remote endpoint error = %v", err)
	}
}

func TestHandleResultAttachesHerdrSessionThroughValidatedJumpRoute(t *testing.T) {
	original := runInteractiveCommand
	var gotName string
	var gotArgs []string
	runInteractiveCommand = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runInteractiveCommand = original })

	err := handleResult(&tui.Result{
		Action:          tui.ActionAttachHerdrSession,
		SessionName:     "agents",
		HerdrEndpointID: "bastion/macmini",
		HerdrTarget:     "macmini",
		HerdrVia:        []string{"user@bastion"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-J", "user@bastion", "-t", "--", "macmini", "herdr", "--session", "agents"}
	if gotName != "ssh" || !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("interactive attach = %s %#v; want ssh %#v", gotName, gotArgs, want)
	}
}

func TestHandleResultUsesSSHForSelectedRemoteSessionInsideHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	original := runInteractiveCommand
	var gotName string
	var gotArgs []string
	runInteractiveCommand = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runInteractiveCommand = original })

	err := handleResult(&tui.Result{
		Action:          tui.ActionAttachHerdrSession,
		SessionName:     "coa_bugs",
		HerdrEndpointID: "macmini",
		HerdrTarget:     "oles@bmo.local",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-t", "--", "oles@bmo.local", "herdr", "--session", "coa_bugs"}
	if gotName != "ssh" || !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("inside-Herdr attach = %s %#v; want ssh %#v", gotName, gotArgs, want)
	}
}

func TestHandleResultKeepsNativeRemoteClientOutsideHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	original := runInteractiveCommand
	var gotName string
	var gotArgs []string
	runInteractiveCommand = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runInteractiveCommand = original })

	err := handleResult(&tui.Result{
		Action:          tui.ActionAttachHerdrSession,
		SessionName:     "coa_bugs",
		HerdrEndpointID: "macmini",
		HerdrTarget:     "oles@bmo.local",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--remote", "oles@bmo.local", "--session", "coa_bugs"}
	if gotName != "herdr" || !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("normal attach = %s %#v; want herdr %#v", gotName, gotArgs, want)
	}
}

func TestRunCLIPassesFlagsToTrackedAgentWithoutPersistingArguments(t *testing.T) {
	paths := configureCLIPaths(t)
	writeFakeAgent(t, "fakeagent", "printf 'agent:%s\\n' \"$*\"\nexit 0\n")

	var out, errOut bytes.Buffer
	code := runCLI([]string{"run", "fakeagent", "--version", "secret-prompt"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("runCLI code = %d, stderr = %q", code, errOut.String())
	}
	if got := out.String(); got != "agent:--version secret-prompt\n" {
		t.Fatalf("agent output = %q", got)
	}

	sessions, err := agent.NewStore(paths.AgentsDir).List()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Agent != "fakeagent" || sessions[0].Status != agent.StatusExited {
		t.Fatalf("tracked sessions = %#v", sessions)
	}
	entries, err := os.ReadDir(paths.AgentsDir)
	if err != nil {
		t.Fatalf("read agents directory: %v", err)
	}
	var sessionFile string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			sessionFile = entry.Name()
			break
		}
	}
	if sessionFile == "" {
		t.Fatal("agent session file was not created")
	}
	data, err := os.ReadFile(filepath.Join(paths.AgentsDir, sessionFile))
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if strings.Contains(string(data), "secret-prompt") || strings.Contains(string(data), "--version") {
		t.Fatalf("session metadata persisted agent arguments: %s", data)
	}
}

func TestRunCLIReturnsTrackedAgentExitCode(t *testing.T) {
	paths := configureCLIPaths(t)
	writeFakeAgent(t, "failingagent", "exit 7\n")

	var out, errOut bytes.Buffer
	code := runCLI([]string{"run", "failingagent"}, &out, &errOut)
	if code != 7 {
		t.Fatalf("runCLI code = %d, want 7; stderr = %q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("wrapper printed redundant child error: %q", errOut.String())
	}
	sessions, err := agent.NewStore(paths.AgentsDir).List()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ExitCode == nil || *sessions[0].ExitCode != 7 {
		t.Fatalf("tracked exit = %#v", sessions)
	}
}

func TestRunCLIAgentsListAndStatus(t *testing.T) {
	paths := configureCLIPaths(t)
	store := agent.NewStore(paths.AgentsDir)
	session := agent.NewSession("codex", "/repo", agent.Context{Mux: "herdr", HerdrSession: "tatami-repo"})
	session.PID = os.Getpid()
	if err := store.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var listOut, errOut bytes.Buffer
	if code := runCLI([]string{"agents", "list"}, &listOut, &errOut); code != 0 {
		t.Fatalf("agents list code = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{"codex", "running", "herdr", "tatami-repo", "/repo"} {
		if !strings.Contains(listOut.String(), want) {
			t.Fatalf("agents list missing %q in:\n%s", want, listOut.String())
		}
	}

	var statusOut bytes.Buffer
	if code := runCLI([]string{"agents", "status", shortAgentID(session.ID)}, &statusOut, &errOut); code != 0 {
		t.Fatalf("agents status code = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{session.ID, "Agent:   codex", "Session: tatami-repo"} {
		if !strings.Contains(statusOut.String(), want) {
			t.Fatalf("agents status missing %q in:\n%s", want, statusOut.String())
		}
	}
}

func TestRunCLIRejectsMissingRunCommand(t *testing.T) {
	configureCLIPaths(t)
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run"}, &out, &errOut); code != 1 {
		t.Fatalf("runCLI code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "usage: tatami run <agent> [args...]") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestRunCLIPrintsVersionedFederationInventory(t *testing.T) {
	paths := configureCLIPaths(t)
	store, err := workspace.NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&workspace.Workspace{Name: "API", Path: "/srv/api", Folder: "work", QuickAccess: true}); err != nil {
		t.Fatal(err)
	}
	if err := herdrhub.NewStore(paths.HerdrHostsFile).Save([]herdrhub.Endpoint{{ID: "macmini", Label: "Mac Mini", Target: "macmini"}}); err != nil {
		t.Fatal(err)
	}
	original := listHerdrSessionsForInventory
	listHerdrSessionsForInventory = func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "agents", Running: true}}, nil
	}
	t.Cleanup(func() { listHerdrSessionsForInventory = original })

	var out, errOut bytes.Buffer
	if code := runCLI([]string{"hub", "inventory", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("hub inventory code=%d stderr=%q", code, errOut.String())
	}
	inventory, err := herdrhub.ParseInventory(out.Bytes())
	if err != nil {
		t.Fatalf("parse output %q: %v", out.String(), err)
	}
	if len(inventory.Workspaces) != 1 || inventory.Workspaces[0].Name != "API" || len(inventory.Sessions) != 1 || inventory.Sessions[0].Name != "agents" || len(inventory.Hosts) != 1 || inventory.Hosts[0].ID != "macmini" {
		t.Fatalf("inventory = %#v", inventory)
	}

	out.Reset()
	errOut.Reset()
	if code := runCLI([]string{"hub", "inventory"}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "usage: tatami hub inventory --json") {
		t.Fatalf("invalid hub command code=%d stderr=%q", code, errOut.String())
	}
}

func TestRunCLIInventorySurvivesUnavailableHerdr(t *testing.T) {
	paths := configureCLIPaths(t)
	store, err := workspace.NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&workspace.Workspace{Name: "API", Path: "/srv/api"}); err != nil {
		t.Fatal(err)
	}
	original := listHerdrSessionsForInventory
	listHerdrSessionsForInventory = func() ([]shell.HerdrSession, error) {
		return nil, errors.New("Herdr unavailable")
	}
	t.Cleanup(func() { listHerdrSessionsForInventory = original })

	var out, errOut bytes.Buffer
	if code := runCLI([]string{"hub", "inventory", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("hub inventory code=%d stderr=%q", code, errOut.String())
	}
	inventory, err := herdrhub.ParseInventory(out.Bytes())
	if err != nil || len(inventory.Workspaces) != 1 || len(inventory.Sessions) != 0 {
		t.Fatalf("inventory=%#v err=%v", inventory, err)
	}
}

func TestRunCLIPrintsHerdrResourceUsage(t *testing.T) {
	original := collectHerdrResources
	collectHerdrResources = func() (systemusage.Report, error) {
		return systemusage.Report{
			Sessions: []systemusage.SessionUsage{{
				Name: "agentic",
				Agents: []systemusage.AgentUsage{
					{
						Kind: "claude", Status: "idle", CWD: "/repo", PaneID: "w2:p1", Resolved: true,
						CPUPercent: 25, RSSBytes: 512 * 1024 * 1024, ProcessCount: 9, Age: 90 * time.Minute,
					},
					{
						Kind: "codex", Status: "working", CWD: "/repo/worktree", PaneID: "w2:p2",
						UnavailableReason: "process info unavailable: pane occupant changed",
					},
				},
				CPUPercent: 25, RSSBytes: 512 * 1024 * 1024, ProcessCount: 9,
			}},
			CPUPercent:       25,
			HostCPUPercent:   3.125,
			RSSBytes:         512 * 1024 * 1024,
			ProcessCount:     9,
			TotalMemoryBytes: 16 * 1024 * 1024 * 1024,
			LogicalCPUs:      8,
			ResolvedAgents:   1,
			UnresolvedAgents: 1,
		}, nil
	}
	t.Cleanup(func() { collectHerdrResources = original })

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runCLI([]string{"resources"}, &out, &errOut); code != 0 {
		t.Fatalf("runCLI code = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{
		"SESSION", "agentic", "claude", "idle", "25.0%", "512 MiB", "9", "1h30m0s", "w2:p1", "/repo",
		"codex", "unavailable", "pane occupant changed", "3.1% of 8 logical CPUs", "3.1% of 16 GiB RAM",
		"1/2 agents resolved", "summed RSS", "CPU may exceed 100%", "not parsed, printed, or persisted",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("resource output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunCLIPrintsEmptyHerdrResourceUsage(t *testing.T) {
	original := collectHerdrResources
	collectHerdrResources = func() (systemusage.Report, error) {
		return systemusage.Report{LogicalCPUs: 8, TotalMemoryBytes: 16 * 1024 * 1024 * 1024}, nil
	}
	t.Cleanup(func() { collectHerdrResources = original })

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runCLI([]string{"resources"}, &out, &errOut); code != 0 {
		t.Fatalf("runCLI code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No running Herdr agents found") {
		t.Fatalf("empty resource output = %q", out.String())
	}
}

func TestRunCLIReportsHerdrResourceFailure(t *testing.T) {
	original := collectHerdrResources
	collectHerdrResources = func() (systemusage.Report, error) {
		return systemusage.Report{}, errors.New("herdr socket unavailable")
	}
	t.Cleanup(func() { collectHerdrResources = original })

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runCLI([]string{"resources"}, &out, &errOut); code != 1 {
		t.Fatalf("runCLI code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "herdr socket unavailable") {
		t.Fatalf("resource stderr = %q", errOut.String())
	}
}

func configureCLIPaths(t *testing.T) *config.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "")
	t.Setenv("HERDR_ENV", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	paths, err := config.GetPaths()
	if err != nil {
		t.Fatalf("GetPaths: %v", err)
	}
	return paths
}

func writeFakeAgent(t *testing.T, name, body string) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	path := filepath.Join(binDir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
