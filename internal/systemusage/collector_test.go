package systemusage

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OleksandrBesan/tatami/internal/shell"
)

func TestCollectorUsesRunningHerdrSessionsAndKeepsUnavailableAgents(t *testing.T) {
	snapshotCalls := 0
	sleepCalls := 0
	collector := Collector{
		listSessions: func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{
				{Name: "team", Running: true},
				{Name: "old", Running: false},
			}, nil
		},
		listAgents: func(session string) ([]shell.HerdrAgent, error) {
			if session != "team" {
				t.Fatalf("listed agents for stopped session %q", session)
			}
			return []shell.HerdrAgent{
				{Kind: "claude", Status: "idle", CWD: "/repo", PaneID: "w1:p1"},
				{Kind: "codex", Status: "working", CWD: "/repo", PaneID: "w1:p2"},
			}, nil
		},
		processInfo: func(session, paneID string) (shell.HerdrPaneProcessInfo, error) {
			if session != "team" {
				t.Fatalf("inspected pane in stopped session %q", session)
			}
			if paneID == "w1:p2" {
				return shell.HerdrPaneProcessInfo{}, errors.New("pane occupant changed")
			}
			return shell.HerdrPaneProcessInfo{
				PaneID: paneID, ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100},
			}, nil
		},
		snapshot: func() ([]ProcessSample, error) {
			snapshotCalls++
			cpu := 1.0
			rss := uint64(0)
			if snapshotCalls == 2 {
				cpu = 1.25
				rss = 300 * mebibyte
			}
			return []ProcessSample{{PID: 100, PPID: 1, StartedAtMillis: 1_000, CPUSeconds: cpu, RSSBytes: rss}}, nil
		},
		totalMemory: func() (uint64, error) { return 8 * gibibyte, nil },
		logicalCPUs: func() int { return 4 },
		sleep: func(duration time.Duration) {
			sleepCalls++
			if duration != 200*time.Millisecond {
				t.Fatalf("sample interval = %s", duration)
			}
		},
		now:            func() time.Time { return time.Unix(10, 0) },
		sampleInterval: 200 * time.Millisecond,
	}

	report, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if snapshotCalls != 2 || sleepCalls != 1 {
		t.Fatalf("sampling calls = snapshots %d, sleeps %d", snapshotCalls, sleepCalls)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].Name != "team" || len(report.Sessions[0].Agents) != 2 {
		t.Fatalf("report sessions = %#v", report.Sessions)
	}
	resolved := report.Sessions[0].Agents[0]
	if !resolved.Resolved || resolved.ProcessCount != 1 || resolved.RSSBytes != 300*mebibyte {
		t.Fatalf("resolved agent = %#v", resolved)
	}
	if !closeEnough(resolved.CPUPercent, 125) {
		t.Fatalf("resolved CPU = %.3f%%, want 125%%", resolved.CPUPercent)
	}
	unavailable := report.Sessions[0].Agents[1]
	if unavailable.Resolved || !strings.Contains(unavailable.UnavailableReason, "pane occupant changed") {
		t.Fatalf("unavailable agent = %#v", unavailable)
	}
}

func TestCollectorReturnsWithoutSamplingWhenThereAreNoRunningAgents(t *testing.T) {
	collector := Collector{
		listSessions: func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{{Name: "old", Running: false}}, nil
		},
		snapshot: func() ([]ProcessSample, error) {
			t.Fatal("sampled processes without running agents")
			return nil, nil
		},
		totalMemory: func() (uint64, error) { return 8 * gibibyte, nil },
		logicalCPUs: func() int { return 4 },
	}

	report, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(report.Sessions) != 0 || report.TotalMemoryBytes != 8*gibibyte || report.LogicalCPUs != 4 {
		t.Fatalf("empty report = %#v", report)
	}
}

func TestCollectorCollectSessionOnlyQueriesRequestedSession(t *testing.T) {
	snapshotCalls := 0
	collector := Collector{
		listSessions: func() ([]shell.HerdrSession, error) {
			t.Fatal("single-session collection listed the session inventory")
			return nil, nil
		},
		listAgents: func(session string) ([]shell.HerdrAgent, error) {
			if session != "team" {
				t.Fatalf("listed agents for session %q; want team", session)
			}
			return []shell.HerdrAgent{{Kind: "claude", Status: "idle", PaneID: "w1:p1"}}, nil
		},
		processInfo: func(session, paneID string) (shell.HerdrPaneProcessInfo, error) {
			if session != "team" || paneID != "w1:p1" {
				t.Fatalf("process info request = %q %q", session, paneID)
			}
			return shell.HerdrPaneProcessInfo{
				ForegroundProcessGroupID: 100,
				ForegroundPIDs:           []int32{100},
			}, nil
		},
		snapshot: func() ([]ProcessSample, error) {
			snapshotCalls++
			cpu := float64(snapshotCalls)
			return []ProcessSample{{
				PID: 100, PPID: 1, StartedAtMillis: 1_000, CPUSeconds: cpu, RSSBytes: 200 * mebibyte,
			}}, nil
		},
		totalMemory:    func() (uint64, error) { return 8 * gibibyte, nil },
		logicalCPUs:    func() int { return 4 },
		sleep:          func(time.Duration) {},
		now:            func() time.Time { return time.Unix(10, 0) },
		sampleInterval: time.Second,
	}

	report, err := collector.CollectSession("team")
	if err != nil {
		t.Fatalf("CollectSession returned error: %v", err)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].Name != "team" {
		t.Fatalf("single-session report = %#v", report.Sessions)
	}
	if report.Sessions[0].RSSBytes != 200*mebibyte || !closeEnough(report.Sessions[0].CPUPercent, 100) {
		t.Fatalf("single-session totals = %#v", report.Sessions[0])
	}
	if snapshotCalls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", snapshotCalls)
	}
}

func TestCollectorCollectSessionRejectsBlankName(t *testing.T) {
	if _, err := (Collector{}).CollectSession("  "); err == nil {
		t.Fatal("blank Herdr session name was accepted")
	}
}

func TestCollectorPropagatesHerdrAndSnapshotErrors(t *testing.T) {
	t.Run("sessions", func(t *testing.T) {
		collector := Collector{listSessions: func() ([]shell.HerdrSession, error) {
			return nil, errors.New("socket unavailable")
		}}
		if _, err := collector.Collect(); err == nil || !strings.Contains(err.Error(), "socket unavailable") {
			t.Fatalf("Collect error = %v", err)
		}
	})

	t.Run("agents", func(t *testing.T) {
		collector := Collector{
			listSessions: func() ([]shell.HerdrSession, error) {
				return []shell.HerdrSession{{Name: "team", Running: true}}, nil
			},
			listAgents: func(string) ([]shell.HerdrAgent, error) {
				return nil, errors.New("agent request failed")
			},
		}
		if _, err := collector.Collect(); err == nil || !strings.Contains(err.Error(), "agent request failed") {
			t.Fatalf("Collect error = %v", err)
		}
	})

	t.Run("snapshot", func(t *testing.T) {
		collector := Collector{
			listSessions: func() ([]shell.HerdrSession, error) {
				return []shell.HerdrSession{{Name: "team", Running: true}}, nil
			},
			listAgents: func(string) ([]shell.HerdrAgent, error) {
				return []shell.HerdrAgent{{Kind: "claude", PaneID: "w1:p1"}}, nil
			},
			processInfo: func(string, string) (shell.HerdrPaneProcessInfo, error) {
				return shell.HerdrPaneProcessInfo{ForegroundProcessGroupID: 100, ForegroundPIDs: []int32{100}}, nil
			},
			snapshot: func() ([]ProcessSample, error) { return nil, errors.New("process table denied") },
		}
		if _, err := collector.Collect(); err == nil || !strings.Contains(err.Error(), "process table denied") {
			t.Fatalf("Collect error = %v", err)
		}
	})
}
