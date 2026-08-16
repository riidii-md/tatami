package systemusage

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/shell"
)

const defaultSampleInterval = 250 * time.Millisecond

// Collector takes a short process sample and attributes it to running Herdr agents.
type Collector struct {
	listSessions   func() ([]shell.HerdrSession, error)
	listAgents     func(string) ([]shell.HerdrAgent, error)
	processInfo    func(string, string) (shell.HerdrPaneProcessInfo, error)
	snapshot       func() ([]ProcessSample, error)
	totalMemory    func() (uint64, error)
	logicalCPUs    func() int
	sleep          func(time.Duration)
	now            func() time.Time
	sampleInterval time.Duration
}

// NewCollector creates a collector backed by the local Herdr and process table.
func NewCollector() Collector {
	return Collector{
		listSessions:   shell.ListHerdrSessions,
		listAgents:     shell.ListHerdrAgents,
		processInfo:    shell.GetHerdrPaneProcessInfo,
		snapshot:       snapshotProcesses,
		totalMemory:    totalMemoryBytes,
		logicalCPUs:    runtime.NumCPU,
		sleep:          time.Sleep,
		now:            time.Now,
		sampleInterval: defaultSampleInterval,
	}
}

// CollectHerdr takes one resource-usage snapshot of all running Herdr agents.
func CollectHerdr() (Report, error) {
	return NewCollector().Collect()
}

// CollectHerdrSession takes one resource-usage snapshot of a named Herdr session.
func CollectHerdrSession(session string) (Report, error) {
	return NewCollector().CollectSession(session)
}

// Collect builds a resource report. A pane that changes occupants during collection
// is kept in the report as unavailable instead of being attributed speculatively.
func (collector Collector) Collect() (Report, error) {
	collector = collector.withDefaults()
	sessions, err := collector.listSessions()
	if err != nil {
		return Report{}, fmt.Errorf("list herdr sessions: %w", err)
	}

	running := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session.Running {
			running = append(running, session.Name)
		}
	}
	return collector.collectSessions(running)
}

// CollectSession takes one resource snapshot without re-listing the Herdr session inventory.
func (collector Collector) CollectSession(session string) (Report, error) {
	if strings.TrimSpace(session) == "" {
		return Report{}, fmt.Errorf("herdr session name is required")
	}
	collector = collector.withDefaults()
	return collector.collectSessions([]string{session})
}

func (collector Collector) collectSessions(sessions []string) (Report, error) {
	targets := make([]AgentTarget, 0)
	for _, session := range sessions {
		agents, err := collector.listAgents(session)
		if err != nil {
			return Report{}, fmt.Errorf("list agents in herdr session %s: %w", session, err)
		}
		for _, agent := range agents {
			target := AgentTarget{
				Session: session,
				Kind:    agent.Kind,
				Status:  agent.Status,
				CWD:     agent.CWD,
				PaneID:  agent.PaneID,
			}
			info, err := collector.processInfo(session, agent.PaneID)
			if err != nil {
				target.UnavailableReason = fmt.Sprintf("process info unavailable: %v", err)
			} else {
				target.ForegroundProcessGroupID = info.ForegroundProcessGroupID
				target.ForegroundPIDs = append([]int32(nil), info.ForegroundPIDs...)
			}
			targets = append(targets, target)
		}
	}

	totalMemory, err := collector.totalMemory()
	if err != nil {
		return Report{}, fmt.Errorf("read total memory: %w", err)
	}
	logicalCPUs := collector.logicalCPUs()
	if len(targets) == 0 {
		return Report{TotalMemoryBytes: totalMemory, LogicalCPUs: logicalCPUs}, nil
	}

	before, err := collector.snapshot()
	if err != nil {
		return Report{}, fmt.Errorf("take initial process snapshot: %w", err)
	}
	collector.sleep(collector.sampleInterval)
	after, err := collector.snapshot()
	if err != nil {
		return Report{}, fmt.Errorf("take final process snapshot: %w", err)
	}
	return BuildReport(targets, before, after, collector.sampleInterval, totalMemory, logicalCPUs, collector.now()), nil
}

func (collector Collector) withDefaults() Collector {
	defaults := NewCollector()
	if collector.listSessions == nil {
		collector.listSessions = defaults.listSessions
	}
	if collector.listAgents == nil {
		collector.listAgents = defaults.listAgents
	}
	if collector.processInfo == nil {
		collector.processInfo = defaults.processInfo
	}
	if collector.snapshot == nil {
		collector.snapshot = defaults.snapshot
	}
	if collector.totalMemory == nil {
		collector.totalMemory = defaults.totalMemory
	}
	if collector.logicalCPUs == nil {
		collector.logicalCPUs = defaults.logicalCPUs
	}
	if collector.sleep == nil {
		collector.sleep = defaults.sleep
	}
	if collector.now == nil {
		collector.now = defaults.now
	}
	if collector.sampleInterval <= 0 {
		collector.sampleInterval = defaults.sampleInterval
	}
	return collector
}
