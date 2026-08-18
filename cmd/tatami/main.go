package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/OleksandrBesan/tatami/internal/agent"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var version = "dev"
var collectHerdrResources = systemusage.CollectHerdr

type launchOptions struct {
	newTabMode  bool
	mobileMode  bool
	showVersion bool
}

func parseLaunchOptions(args []string) launchOptions {
	var options launchOptions
	for _, arg := range args {
		switch arg {
		case "--version", "-v":
			options.showVersion = true
		case "--new-tab":
			options.newTabMode = true
		case "--mobile", "-m":
			options.mobileMode = true
		}
	}
	return options
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "resources" {
		if err := handleResourcesCommand(args[1:], out); err != nil {
			fmt.Fprintf(errOut, "Error: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) > 0 && (args[0] == "run" || args[0] == "agents") {
		paths, err := config.GetPaths()
		if err == nil {
			err = handleTopLevelCommand(args, paths, os.Stdin, out, errOut)
		}
		if err != nil {
			var exitErr *trackedAgentExitError
			if errors.As(err, &exitErr) {
				return exitErr.code
			}
			fmt.Fprintf(errOut, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	options := parseLaunchOptions(args)
	if options.showVersion {
		fmt.Fprintf(out, "tatami %s\n", version)
		return 0
	}

	if err := run(options.newTabMode, options.mobileMode); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	return 0
}

func handleResourcesCommand(args []string, out io.Writer) error {
	if len(args) > 0 {
		return errors.New("usage: tatami resources")
	}
	report, err := collectHerdrResources()
	if err != nil {
		return err
	}
	return printHerdrResources(report, out)
}

func printHerdrResources(report systemusage.Report, out io.Writer) error {
	if len(report.Sessions) == 0 {
		fmt.Fprintln(out, "No running Herdr agents found.")
		return nil
	}

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "SESSION\tAGENT\tSTATUS\tCPU\tRAM\tPROCS\tAGE\tPANE\tCWD\tNOTE")
	for _, session := range report.Sessions {
		for _, agent := range session.Agents {
			if !agent.Resolved {
				fmt.Fprintf(
					table,
					"%s\t%s\t%s\t-\t-\t-\t-\t%s\t%s\tunavailable: %s\n",
					session.Name,
					valueOrDash(agent.Kind),
					valueOrDash(agent.Status),
					valueOrDash(agent.PaneID),
					valueOrDash(agent.CWD),
					valueOrDash(agent.UnavailableReason),
				)
				continue
			}
			fmt.Fprintf(
				table,
				"%s\t%s\t%s\t%.1f%%\t%s\t%d\t%s\t%s\t%s\t\n",
				session.Name,
				valueOrDash(agent.Kind),
				valueOrDash(agent.Status),
				agent.CPUPercent,
				formatIECBytes(agent.RSSBytes),
				agent.ProcessCount,
				agent.Age.Round(time.Second),
				valueOrDash(agent.PaneID),
				valueOrDash(agent.CWD),
			)
		}
		fmt.Fprintf(
			table,
			"%s\tTOTAL\t\t%.1f%%\t%s\t%d\t\t\t\t\n",
			session.Name,
			session.CPUPercent,
			formatIECBytes(session.RSSBytes),
			session.ProcessCount,
		)
	}
	if err := table.Flush(); err != nil {
		return err
	}

	totalAgents := report.ResolvedAgents + report.UnresolvedAgents
	ramPercent := float64(0)
	if report.TotalMemoryBytes > 0 {
		ramPercent = float64(report.RSSBytes) / float64(report.TotalMemoryBytes) * 100
	}
	fmt.Fprintf(
		out,
		"\nTracked total: CPU %.1f%% (%.1f%% of %d logical CPUs), RAM %s (%.1f%% of %s RAM), %d processes, %d/%d agents resolved.\n",
		report.CPUPercent,
		report.HostCPUPercent,
		report.LogicalCPUs,
		formatIECBytes(report.RSSBytes),
		ramPercent,
		formatIECBytes(report.TotalMemoryBytes),
		report.ProcessCount,
		report.ResolvedAgents,
		totalAgents,
	)
	fmt.Fprintln(out, "Note: RAM is summed RSS and can double-count shared pages; CPU may exceed 100% across cores.")
	fmt.Fprintln(out, "Process arguments and prompts are not parsed, printed, or persisted.")
	return nil
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatIECBytes(bytes uint64) string {
	const unit = uint64(1024)
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor := unit
	unitName := "KiB"
	for _, candidate := range []string{"MiB", "GiB", "TiB", "PiB"} {
		if bytes < divisor*unit {
			break
		}
		divisor *= unit
		unitName = candidate
	}
	value := float64(bytes) / float64(divisor)
	if value >= 10 || value == float64(uint64(value)) {
		return fmt.Sprintf("%.0f %s", value, unitName)
	}
	return fmt.Sprintf("%.1f %s", value, unitName)
}

func run(newTabMode, mobileMode bool) error {
	// Load config paths
	paths, err := config.GetPaths()
	if err != nil {
		return fmt.Errorf("failed to get config paths: %w", err)
	}

	// Initialize workspace store
	store, err := workspace.NewStore(paths)
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}
	hubEndpoints, hubCache, hubCacheWritable, err := loadHerdrHub(paths)
	if err != nil {
		return err
	}

	// Prevent lipgloss from blocking on OSC 11 terminal background color query.
	// Some terminals don't respond to OSC queries, causing a 5s hang on first render.
	lipgloss.SetHasDarkBackground(true)

	// Create and run the TUI app
	appOptions := []tui.AppOption{}
	if newTabMode {
		appOptions = append(appOptions, tui.WithNewTabMode())
	}
	if mobileMode {
		appOptions = append(appOptions, tui.WithMobileMode())
	}
	appOptions = append(appOptions, tui.WithHerdrHubSnapshots(hubEndpoints, hubCache.Snapshots))
	herdrHubClient := herdrhub.NewClient(nil)
	var saveHubCache func(herdrhub.Cache) error
	if hubCacheWritable {
		saveHubCache = func(cache herdrhub.Cache) error { return herdrhub.SaveCache(paths.HerdrHubFile, cache) }
	}
	appOptions = append(appOptions, tui.WithHerdrHubRefresh(func(ctx context.Context, endpoints []herdrhub.Endpoint, previous herdrhub.Cache) []herdrhub.Snapshot {
		return herdrhub.RefreshWithTimeout(ctx, herdrHubClient, endpoints, previous, 1, 5*time.Second)
	}, saveHubCache))
	appOptions = append(appOptions, tui.WithHerdrHubEndpointSaver(func(endpoints []herdrhub.Endpoint) error {
		return herdrhub.NewStore(paths.HerdrHostsFile).Save(endpoints)
	}))
	appOptions = append(appOptions, tui.WithHerdrHubAgentQuery(func(ctx context.Context, endpoint herdrhub.Endpoint, session string) ([]herdrhub.Agent, error) {
		return herdrHubClient.QueryAgents(ctx, endpoint, session)
	}))
	appOptions = append(appOptions, tui.WithHerdrHubInteractiveSessionLister(func(ctx context.Context, endpoint herdrhub.Endpoint, stdin io.Reader, stderr io.Writer) ([]herdrhub.Session, error) {
		return herdrHubClient.QueryInteractive(ctx, endpoint, stdin, stderr)
	}))
	app := tui.NewApp(store, appOptions...)

	// When running inside the shell wrapper (TATAMI_WRAPPER=1), stdout is redirected
	// to a temp file to capture the result path. We must attach the TUI to /dev/tty
	// directly so the UI renders in the terminal regardless of stdout redirection.
	var opts []tea.ProgramOption
	opts = append(opts, tea.WithAltScreen())
	if os.Getenv("TATAMI_WRAPPER") == "1" {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err == nil {
			opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
			defer tty.Close()
		}
	}
	p := tea.NewProgram(app, opts...)

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	// Handle result
	finalApp, ok := finalModel.(*tui.App)
	if !ok {
		return nil
	}

	result := finalApp.Result()
	if result == nil {
		return nil
	}

	return handleResult(result, newTabMode)
}

// loadHerdrHub treats host configuration as authoritative and the inventory as
// disposable state. A corrupt cache is ignored for this run, but cache writes
// are disabled so the original bytes remain available for diagnosis.
func loadHerdrHub(paths *config.Paths) ([]herdrhub.Endpoint, herdrhub.Cache, bool, error) {
	endpoints, err := herdrhub.NewStore(paths.HerdrHostsFile).List()
	if err != nil {
		return nil, herdrhub.Cache{}, false, fmt.Errorf("failed to load Herdr hosts configuration (not modified): %w", err)
	}
	cache, err := herdrhub.LoadCache(paths.HerdrHubFile)
	if err != nil {
		return endpoints, herdrhub.Cache{}, false, nil
	}
	return endpoints, cache, true, nil
}

func handleTopLevelCommand(args []string, paths *config.Paths, in io.Reader, out, errOut io.Writer) error {
	switch args[0] {
	case "run":
		if len(args) < 2 {
			return errors.New("usage: tatami run <agent> [args...]")
		}
		return runTrackedAgent(paths, args[1], args[2:], in, out, errOut)
	case "agents":
		return handleAgentsCommand(paths, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func handleAgentsCommand(paths *config.Paths, args []string, out io.Writer) error {
	command := "list"
	if len(args) > 0 {
		command = args[0]
	}
	store := agent.NewStore(paths.AgentsDir)
	switch command {
	case "list", "ls":
		return printAgents(store, out)
	case "status":
		if len(args) < 2 {
			return errors.New("usage: tatami agents status <id>")
		}
		if _, err := store.PruneStale(); err != nil {
			return err
		}
		session, err := store.Get(args[1])
		if err != nil {
			return err
		}
		return printAgentDetail(session, out)
	case "prune":
		changed, err := store.PruneStale()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "marked %d stale agent session(s)\n", changed)
		return nil
	default:
		return fmt.Errorf("unknown agents command %q", command)
	}
}

func runTrackedAgent(paths *config.Paths, agentName string, args []string, in io.Reader, out, errOut io.Writer) error {
	binary, err := exec.LookPath(agentName)
	if err != nil {
		return fmt.Errorf("%s not found in PATH: %w", agentName, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	command := exec.Command(binary, args...)
	command.Stdin = in
	command.Stdout = out
	command.Stderr = errOut
	command.Env = os.Environ()
	command.Dir = cwd
	if err := command.Start(); err != nil {
		return fmt.Errorf("start %s: %w", agentName, err)
	}

	session := agent.NewSession(filepath.Base(agentName), cwd, agent.DetectContext())
	session.PID = command.Process.Pid
	store := agent.NewStore(paths.AgentsDir)
	if err := store.Create(session); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("record agent start: %w", err)
	}

	waitErr := command.Wait()
	now := time.Now().UTC()
	session.EndedAt = &now
	session.Status = agent.StatusExited
	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		var processErr *exec.ExitError
		if errors.As(waitErr, &processErr) {
			exitCode = processExitCode(processErr)
		}
	}
	session.ExitCode = &exitCode
	if err := store.Update(session); err != nil {
		return fmt.Errorf("record agent exit: %w", err)
	}
	if waitErr == nil {
		return nil
	}
	var processErr *exec.ExitError
	if errors.As(waitErr, &processErr) {
		return &trackedAgentExitError{code: exitCode, err: waitErr}
	}
	return fmt.Errorf("wait for %s: %w", agentName, waitErr)
}

type trackedAgentExitError struct {
	code int
	err  error
}

func (e *trackedAgentExitError) Error() string { return e.err.Error() }
func (e *trackedAgentExitError) Unwrap() error { return e.err }

func processExitCode(exitErr *exec.ExitError) int {
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

func printAgents(store *agent.Store, out io.Writer) error {
	if _, err := store.PruneStale(); err != nil {
		return err
	}
	sessions, err := store.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "No tracked AI agent sessions yet. Start one with: tatami run claude")
		return nil
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tAGENT\tSTATUS\tMUX\tLOCATION\tPID\tAGE\tCWD")
	for _, session := range sessions {
		fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			shortAgentID(session.ID),
			session.Agent,
			session.Status,
			session.Context.Mux,
			agentLocation(session.Context),
			session.PID,
			agentAge(session.StartedAt),
			session.Cwd,
		)
	}
	return table.Flush()
}

func printAgentDetail(session *agent.Session, out io.Writer) error {
	fmt.Fprintf(out, "ID:      %s\n", session.ID)
	fmt.Fprintf(out, "Agent:   %s\n", session.Agent)
	fmt.Fprintf(out, "Status:  %s\n", session.Status)
	fmt.Fprintf(out, "PID:     %d\n", session.PID)
	fmt.Fprintf(out, "Mux:     %s\n", session.Context.Mux)
	if location := agentLocation(session.Context); location != "" {
		fmt.Fprintf(out, "Session: %s\n", location)
	}
	fmt.Fprintf(out, "Cwd:     %s\n", session.Cwd)
	fmt.Fprintf(out, "Started: %s\n", session.StartedAt.Format(time.RFC3339))
	if session.EndedAt != nil {
		fmt.Fprintf(out, "Ended:   %s\n", session.EndedAt.Format(time.RFC3339))
	}
	if session.ExitCode != nil {
		fmt.Fprintf(out, "Exit:    %d\n", *session.ExitCode)
	}
	return nil
}

func agentLocation(context agent.Context) string {
	switch context.Mux {
	case "herdr":
		return context.HerdrSession
	case "zellij":
		return strings.Trim(strings.Join([]string{context.ZellijSession, context.ZellijPaneID}, "/"), "/")
	case "tmux":
		parts := make([]string, 0, 3)
		for _, part := range []string{context.TmuxSession, context.TmuxWindow, context.TmuxPane} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, ":")
	case "kitty":
		return context.KittyWindowID
	default:
		return context.TTY
	}
}

func shortAgentID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func agentAge(startedAt time.Time) string {
	age := time.Since(startedAt).Round(time.Second)
	if age < 0 {
		return "0s"
	}
	return age.String()
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

type processSpec struct {
	path string
	args []string
	dir  string
}

func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func newTabProcess(ws *workspace.Workspace, shellPath string, lookPath func(string) (string, error)) (processSpec, error) {
	if ws.IsRemote() {
		sshPath, err := lookPath("ssh")
		if err != nil {
			return processSpec{}, fmt.Errorf("ssh not found: %w", err)
		}

		remotePath := ws.Remote.Path
		if remotePath == "" {
			remotePath = ws.Path
		}
		remoteCommand := "exec ${SHELL:-/bin/sh}"
		if remotePath != "" {
			remoteCommand = "cd " + quoteShellArg(remotePath) + " && " + remoteCommand
		}

		args := []string{sshPath}
		if ws.Remote.Key != "" {
			args = append(args, "-i", ws.Remote.Key)
		}
		args = append(args, "-t", "--", ws.Remote.Host, remoteCommand)
		return processSpec{path: sshPath, args: args}, nil
	}

	if shellPath == "" {
		shellPath = "/bin/sh"
	}
	resolvedShell, err := lookPath(shellPath)
	if err != nil {
		return processSpec{}, fmt.Errorf("shell %q not found: %w", shellPath, err)
	}
	return processSpec{
		path: resolvedShell,
		args: []string{resolvedShell},
		dir:  ws.Path,
	}, nil
}

func openWorkspaceInNewTab(ws *workspace.Workspace) error {
	process, err := newTabProcess(ws, os.Getenv("SHELL"), exec.LookPath)
	if err != nil {
		return err
	}
	if process.dir != "" {
		if err := os.Chdir(process.dir); err != nil {
			return fmt.Errorf("failed to enter workspace %q: %w", process.dir, err)
		}
	}
	return syscall.Exec(process.path, process.args, os.Environ())
}

func newTabTarget(result *tui.Result) (*workspace.Workspace, bool) {
	if result == nil || result.Workspace == nil {
		return nil, false
	}
	if result.Action == tui.ActionCD {
		return result.Workspace, true
	}
	if result.Action != tui.ActionWorktree || result.Worktree == nil || result.Template == nil {
		return nil, false
	}
	if result.Template.MainCmd != "" || len(result.Template.Panes) > 0 {
		return nil, false
	}

	target := *result.Workspace
	target.Name = result.Worktree.Branch
	if target.Name == "" {
		target.Name = "worktree"
	}
	target.Path = result.Worktree.Path
	target.Remote = nil
	return &target, true
}

func herdrTarget(result *tui.Result) (*workspace.Workspace, bool) {
	if result == nil || result.Workspace == nil || result.Workspace.Layout.Type != workspace.LayoutHerdr {
		return nil, false
	}

	target := *result.Workspace
	switch result.Action {
	case tui.ActionWithLayout, tui.ActionCD:
		return &target, true

	case tui.ActionWithTemplate:
		if result.Template == nil {
			return nil, false
		}
		target.Layout = workspace.Layout{
			Type:    workspace.LayoutHerdr,
			MainCmd: result.Template.MainCmd,
			Panes:   append([]workspace.Pane(nil), result.Template.Panes...),
		}
		return &target, true

	case tui.ActionWorktree:
		if result.Worktree == nil {
			return nil, false
		}
		target.Name = result.Worktree.Branch
		if target.Name == "" {
			target.Name = "worktree"
		}
		target.Path = result.Worktree.Path
		target.Remote = nil
		return &target, true
	}

	return nil, false
}

func herdrSessionName(result *tui.Result, target *workspace.Workspace) string {
	if result != nil && result.HerdrMode == tui.HerdrOpenExisting {
		return strings.TrimSpace(result.HerdrSessionName)
	}
	if result != nil && strings.TrimSpace(result.HerdrSessionName) != "" {
		return strings.TrimSpace(result.HerdrSessionName)
	}
	return shell.SessionName(target.Name)
}

func handleResult(result *tui.Result, newTabMode bool) error {
	// Handle session attachment first (doesn't need workspace)
	if result.Action == tui.ActionAttachSession {
		if result.SessionName == "" {
			return fmt.Errorf("no session selected")
		}
		// Use syscall.Exec to replace current process with zellij attach
		// This is necessary because the TUI uses alternate screen and
		// zellij attach needs full terminal control
		zellijPath, err := exec.LookPath("zellij")
		if err != nil {
			return fmt.Errorf("zellij not found: %w", err)
		}
		args := shell.AttachSessionCmd(result.SessionName)
		return syscall.Exec(zellijPath, args, os.Environ())
	}
	if result.Action == tui.ActionAttachHerdrEndpoint {
		endpoint := herdrhub.Endpoint{ID: result.HerdrEndpointID, Label: result.HerdrEndpointID, Target: result.HerdrTarget}
		if err := herdrhub.ValidateEndpoint(endpoint); err != nil {
			return fmt.Errorf("invalid remote Herdr endpoint: %w", err)
		}
		if err := shell.NewHerdrRunner().AttachRemote(endpoint.Target); err != nil {
			return fmt.Errorf("open Herdr endpoint %q: %w", endpoint.ID, err)
		}
		return nil
	}
	if result.Action == tui.ActionAttachHerdrSession {
		if result.SessionName == "" {
			return fmt.Errorf("no Herdr session selected")
		}
		if result.HerdrEndpointID == "" || result.HerdrEndpointID == herdrhub.LocalEndpointID {
			return shell.NewHerdrRunner().AttachSession(result.SessionName)
		}
		endpoint := herdrhub.Endpoint{ID: result.HerdrEndpointID, Label: result.HerdrEndpointID, Target: result.HerdrTarget}
		if _, _, err := herdrhub.AttachArgs(endpoint, result.SessionName); err != nil {
			return fmt.Errorf("invalid remote Herdr endpoint: %w", err)
		}
		if err := shell.NewHerdrRunner().AttachRemoteSession(endpoint.Target, result.SessionName); err != nil {
			return fmt.Errorf("attach Herdr session %q on endpoint %q: %w", result.SessionName, endpoint.ID, err)
		}
		return nil
	}

	ws := result.Workspace
	if ws == nil {
		return fmt.Errorf("no workspace selected")
	}
	if ws.Layout.Type == workspace.LayoutHerdr {
		if ws.IsRemote() {
			return fmt.Errorf("herdr layout backend is only supported for local workspaces")
		}
		target, ok := herdrTarget(result)
		if !ok {
			return fmt.Errorf("action is not supported by the herdr backend")
		}
		session := herdrSessionName(result, target)
		if session == "" {
			return fmt.Errorf("no Herdr session selected")
		}
		return shell.NewHerdrRunner().RunWithLayoutInSession(target, session)
	}
	if newTabMode {
		if target, ok := newTabTarget(result); ok {
			return openWorkspaceInNewTab(target)
		}
	}

	zellij := shell.NewZellijRunner()
	tmux := shell.NewTmuxRunner()
	isRemote := ws.IsRemote()

	switch result.Action {
	case tui.ActionCD:
		if isRemote {
			// For remote, SSH to the host
			var sshCmd string
			if ws.Remote.Key != "" {
				sshCmd = fmt.Sprintf("ssh -i %s %s -t 'cd %s && exec $SHELL'", ws.Remote.Key, ws.Remote.Host, ws.Remote.Path)
			} else {
				sshCmd = fmt.Sprintf("ssh %s -t 'cd %s && exec $SHELL'", ws.Remote.Host, ws.Remote.Path)
			}
			if zellij.IsInsideSession() {
				return zellij.WriteChars(sshCmd + "\n")
			}
			if tmux.IsInsideSession() {
				return tmux.SendKeys(sshCmd)
			}
			if err := copyToClipboard(sshCmd); err == nil {
				fmt.Printf("%s  (copied to clipboard, paste to run)\n", sshCmd)
			} else {
				fmt.Println(sshCmd)
			}
			return nil
		}

		// Local workspace
		if os.Getenv("TATAMI_WRAPPER") == "1" {
			fmt.Println(ws.Path)
			return nil
		}
		if zellij.IsInsideSession() {
			return zellij.WriteChars(fmt.Sprintf("cd %s\n", ws.Path))
		}
		if tmux.IsInsideSession() {
			return tmux.SendKeys(fmt.Sprintf("cd %s", ws.Path))
		}
		cdCmd := fmt.Sprintf("cd %s", ws.Path)
		if err := copyToClipboard(cdCmd); err == nil {
			fmt.Printf("%s  (copied to clipboard, paste to run)\n", cdCmd)
		} else {
			fmt.Println(cdCmd)
		}
		return nil

	case tui.ActionNewTab:
		if isRemote {
			if zellij.IsInsideSession() {
				return zellij.NewTabSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, ws.Name)
			}
			if tmux.IsInsideSession() {
				return tmux.NewWindowSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, ws.Name)
			}
		} else {
			if zellij.IsInsideSession() {
				return zellij.NewTab(ws.Path, ws.Name)
			}
			if tmux.IsInsideSession() {
				return tmux.NewWindow(ws.Path, ws.Name)
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionNewPane:
		if isRemote {
			if zellij.IsInsideSession() {
				return zellij.NewPaneSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, "down")
			}
			if tmux.IsInsideSession() {
				return tmux.NewPaneSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, "down")
			}
		} else {
			if zellij.IsInsideSession() {
				return zellij.NewPane(ws.Path, "down")
			}
			if tmux.IsInsideSession() {
				return tmux.NewPane(ws.Path, "down")
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionWithLayout:
		if isRemote {
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayoutSSH(ws)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayoutSSH(ws)
			}
		} else {
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayout(ws)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayout(ws)
			}
		}
		fmt.Fprintf(os.Stderr, "Layout type mismatch or not inside session\n")
		return nil

	case tui.ActionWithTemplate:
		if result.Template == nil {
			return fmt.Errorf("no template selected")
		}
		tmplWs := &workspace.Workspace{
			Name:   ws.Name,
			Path:   ws.Path,
			Remote: ws.Remote,
			Layout: workspace.Layout{
				MainCmd: result.Template.MainCmd,
				Panes:   result.Template.Panes,
			},
		}
		if isRemote {
			if zellij.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayoutSSH(tmplWs)
			}
			if tmux.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayoutSSH(tmplWs)
			}
		} else {
			if zellij.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(tmplWs)
			}
			if tmux.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(tmplWs)
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionWorktree:
		if result.Worktree == nil {
			return fmt.Errorf("no worktree selected")
		}
		wt := result.Worktree
		tabName := wt.Branch
		if tabName == "" {
			tabName = "worktree"
		}

		// If template selected with panes, run with template layout
		if result.Template != nil && len(result.Template.Panes) > 0 {
			wtWs := &workspace.Workspace{
				Name: tabName,
				Path: wt.Path,
				Layout: workspace.Layout{
					MainCmd: result.Template.MainCmd,
					Panes:   result.Template.Panes,
				},
			}
			if zellij.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(wtWs)
			}
		}

		// If no template but workspace has saved layout, use that
		if result.Template == nil && len(ws.Layout.Panes) > 0 {
			wtWs := &workspace.Workspace{
				Name: tabName,
				Path: wt.Path,
				Layout: workspace.Layout{
					Type:    ws.Layout.Type,
					MainCmd: ws.Layout.MainCmd,
					Panes:   ws.Layout.Panes,
				},
			}
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayout(wtWs)
			}
			// Fallback: use current session type
			if zellij.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(wtWs)
			}
		}

		// Plain - just open new tab
		if zellij.IsInsideSession() {
			return zellij.NewTab(wt.Path, tabName)
		}
		if tmux.IsInsideSession() {
			return tmux.NewWindow(wt.Path, tabName)
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil
	}

	return nil
}
