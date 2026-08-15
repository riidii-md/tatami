package systemusage

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestBuildReportAggregatesForegroundProcessesAndDescendants(t *testing.T) {
	now := time.Unix(2_000, 0)
	started := now.Add(-90 * time.Minute).UnixMilli()
	targets := []AgentTarget{
		{
			Session: "agentic", Kind: "claude", Status: "idle", CWD: "/repo", PaneID: "w2:p1",
			ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100, 101},
		},
		{
			Session: "agentic", Kind: "codex", Status: "working", CWD: "/repo/worktree", PaneID: "w2:p2",
			ForegroundProcessGroupID: 200, ForegroundPIDs: []int32{200},
		},
	}
	before := []ProcessSample{
		{PID: 100, PPID: 1, StartedAtMillis: started, CPUSeconds: 10},
		{PID: 101, PPID: 100, StartedAtMillis: started + 1, CPUSeconds: 2},
		{PID: 102, PPID: 101, StartedAtMillis: started + 2, CPUSeconds: 1},
		{PID: 200, PPID: 1, StartedAtMillis: started + 3, CPUSeconds: 20},
		{PID: 900, PPID: 1, StartedAtMillis: started, CPUSeconds: 50},
	}
	after := []ProcessSample{
		{PID: 100, PPID: 1, StartedAtMillis: started, CPUSeconds: 10.2, RSSBytes: 100 * mebibyte},
		{PID: 101, PPID: 100, StartedAtMillis: started + 1, CPUSeconds: 2.1, RSSBytes: 50 * mebibyte},
		// PID 102 represents an MCP descendant that is not returned directly by Herdr.
		{PID: 102, PPID: 101, StartedAtMillis: started + 2, CPUSeconds: 1.05, RSSBytes: 25 * mebibyte},
		{PID: 200, PPID: 1, StartedAtMillis: started + 3, CPUSeconds: 20.5, RSSBytes: 400 * mebibyte},
		{PID: 900, PPID: 1, StartedAtMillis: started, CPUSeconds: 60, RSSBytes: 2 * gibibyte},
	}

	report := BuildReport(targets, before, after, time.Second, 16*gibibyte, 8, now)
	if len(report.Sessions) != 1 || len(report.Sessions[0].Agents) != 2 {
		t.Fatalf("report sessions = %#v", report.Sessions)
	}
	claude := report.Sessions[0].Agents[0]
	if !claude.Resolved || claude.RootPID != 100 {
		t.Fatalf("Claude resolution = %#v", claude)
	}
	if claude.ProcessCount != 3 || claude.RSSBytes != 175*mebibyte {
		t.Fatalf("Claude resources = processes %d, RSS %d", claude.ProcessCount, claude.RSSBytes)
	}
	if !closeEnough(claude.CPUPercent, 35) {
		t.Fatalf("Claude CPU = %.3f%%, want 35%%", claude.CPUPercent)
	}
	if claude.Age != 90*time.Minute {
		t.Fatalf("Claude age = %s, want 90m", claude.Age)
	}

	session := report.Sessions[0]
	if session.ProcessCount != 4 || session.RSSBytes != 575*mebibyte || !closeEnough(session.CPUPercent, 85) {
		t.Fatalf("session totals = %#v", session)
	}
	if report.ProcessCount != session.ProcessCount || report.RSSBytes != session.RSSBytes || !closeEnough(report.CPUPercent, 85) {
		t.Fatalf("global totals = %#v", report)
	}
	if !closeEnough(report.HostCPUPercent, 10.625) {
		t.Fatalf("host CPU = %.3f%%, want 10.625%%", report.HostCPUPercent)
	}
}

func TestBuildReportMarksConflictingOwnershipUnresolved(t *testing.T) {
	targets := []AgentTarget{
		{Session: "team", Kind: "claude", PaneID: "w1:p1", ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100}},
		{Session: "team", Kind: "codex", PaneID: "w1:p2", ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100}},
	}
	processes := []ProcessSample{{PID: 100, PPID: 1, StartedAtMillis: 1, RSSBytes: mebibyte}}

	report := BuildReport(targets, processes, processes, time.Second, gibibyte, 4, time.Unix(10, 0))
	for _, agent := range report.Sessions[0].Agents {
		if agent.Resolved || !strings.Contains(agent.UnavailableReason, "shared") {
			t.Fatalf("conflicting agent = %#v", agent)
		}
	}
	if report.ResolvedAgents != 0 || report.UnresolvedAgents != 2 || report.ProcessCount != 0 {
		t.Fatalf("conflict totals = %#v", report)
	}
}

func TestBuildReportDoesNotReuseCPUHistoryAcrossPIDReuse(t *testing.T) {
	targets := []AgentTarget{{Session: "team", Kind: "claude", PaneID: "w1:p1", ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100}}}
	before := []ProcessSample{{PID: 100, PPID: 1, StartedAtMillis: 1_000, CPUSeconds: 99}}
	after := []ProcessSample{{PID: 100, PPID: 1, StartedAtMillis: 2_000, CPUSeconds: 0.2, RSSBytes: mebibyte}}

	report := BuildReport(targets, before, after, time.Second, gibibyte, 4, time.Unix(10, 0))
	if got := report.Sessions[0].Agents[0].CPUPercent; got != 0 {
		t.Fatalf("CPU after PID reuse = %.3f%%, want 0%%", got)
	}
}

func TestBuildReportMarksMissingForegroundProcessUnavailable(t *testing.T) {
	targets := []AgentTarget{{Session: "team", Kind: "claude", PaneID: "w1:p1", ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100}}}

	report := BuildReport(targets, nil, nil, time.Second, gibibyte, 4, time.Unix(10, 0))
	agent := report.Sessions[0].Agents[0]
	if agent.Resolved || !strings.Contains(agent.UnavailableReason, "not running") {
		t.Fatalf("missing process agent = %#v", agent)
	}
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 0.0001
}
