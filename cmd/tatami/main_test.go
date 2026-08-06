package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

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
