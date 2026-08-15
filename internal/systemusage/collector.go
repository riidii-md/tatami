package systemusage

import (
	"fmt"
	"runtime"
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

// Collect builds a resource report. A pane that changes occupants during collection
// is kept in the report as unavailable instead of being attributed speculatively.
func (collector Collector) Collect() (Report, error) {
	collector = collector.withDefaults()
	sessions, err := collector.listSessions()
	if err != nil {
		return Report{}, fmt.Errorf("list herdr sessions: %w", err)
	}

	targets := make([]AgentTarget, 0)
	for _, session := range sessions {
		if !session.Running {
			continue
		}
		agents, err := collector.listAgents(session.Name)
		if err != nil {
			return Report{}, fmt.Errorf("list agents in herdr session %s: %w", session.Name, err)
		}
		for _, agent := range agents {
			target := AgentTarget{
				Session: session.Name,
				Kind:    agent.Kind,
				Status:  agent.Status,
				CWD:     agent.CWD,
				PaneID:  agent.PaneID,
			}
			info, err := collector.processInfo(session.Name, agent.PaneID)
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
