package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/agent"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/git"
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
