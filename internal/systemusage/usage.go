// Package systemusage attributes local process resource usage to Herdr agents.
package systemusage

import (
	"fmt"
	"sort"
	"time"
)

const (
	mebibyte = uint64(1024 * 1024)
	gibibyte = uint64(1024 * 1024 * 1024)
)

// AgentTarget is the process identity Herdr reports for an agent pane.
type AgentTarget struct {
	Session                  string
	Kind                     string
	Status                   string
	CWD                      string
	PaneID                   string
	ForegroundProcessGroupID int32
	ForegroundPIDs           []int32
	UnavailableReason        string
}

// ProcessSample is the resource state of one local process at a point in time.
type ProcessSample struct {
	PID             int32
	PPID            int32
	StartedAtMillis int64
	CPUSeconds      float64
	RSSBytes        uint64
}

// AgentUsage is the resource usage attributed to one Herdr agent.
type AgentUsage struct {
	Kind              string
	Status            string
	CWD               string
	PaneID            string
	Resolved          bool
	UnavailableReason string
	RootPID           int32
	CPUPercent        float64
	RSSBytes          uint64
	ProcessCount      int
	Age               time.Duration
}

// SessionUsage groups agent usage for one Herdr session.
type SessionUsage struct {
	Name         string
	Agents       []AgentUsage
	CPUPercent   float64
	RSSBytes     uint64
	ProcessCount int
}

// Report contains resource usage for every running Herdr agent Tatami could resolve.
type Report struct {
	Sessions         []SessionUsage
	CPUPercent       float64
	HostCPUPercent   float64
	RSSBytes         uint64
	ProcessCount     int
	TotalMemoryBytes uint64
	LogicalCPUs      int
	ResolvedAgents   int
	UnresolvedAgents int
}

// BuildReport attributes an operating-system process snapshot to Herdr agent targets.
func BuildReport(targets []AgentTarget, before, after []ProcessSample, elapsed time.Duration, totalMemory uint64, logicalCPUs int, now time.Time) Report {
	report := Report{TotalMemoryBytes: totalMemory, LogicalCPUs: logicalCPUs}
	beforeByPID := indexProcesses(before)
	afterByPID := indexProcesses(after)
	owned := make([]map[int32]struct{}, len(targets))

	for i, target := range targets {
		owned[i] = processTree(target, afterByPID)
	}
	conflicts := conflictingOwners(owned)

	sessionIndexes := make(map[string]int)
	for i, target := range targets {
		sessionIndex, ok := sessionIndexes[target.Session]
		if !ok {
			sessionIndex = len(report.Sessions)
			sessionIndexes[target.Session] = sessionIndex
			report.Sessions = append(report.Sessions, SessionUsage{Name: target.Session})
		}

		agent := AgentUsage{
			Kind:     target.Kind,
			Status:   target.Status,
			CWD:      target.CWD,
			PaneID:   target.PaneID,
			Resolved: true,
		}
		switch {
		case target.UnavailableReason != "":
			agent.Resolved = false
			agent.UnavailableReason = target.UnavailableReason
		case len(owned[i]) == 0:
			agent.Resolved = false
			agent.UnavailableReason = "foreground process is not running"
		case conflicts[i]:
			agent.Resolved = false
			agent.UnavailableReason = "process ownership is shared with another Herdr agent"
		default:
			agent.RootPID = rootPID(target, owned[i], afterByPID)
			agent.ProcessCount = len(owned[i])
			for pid := range owned[i] {
				current := afterByPID[pid]
				agent.RSSBytes += current.RSSBytes
				previous, found := beforeByPID[pid]
				if found && previous.StartedAtMillis == current.StartedAtMillis {
					delta := current.CPUSeconds - previous.CPUSeconds
					if delta > 0 && elapsed > 0 {
						agent.CPUPercent += delta / elapsed.Seconds() * 100
					}
				}
			}
			if root, found := afterByPID[agent.RootPID]; found {
				startedAt := time.UnixMilli(root.StartedAtMillis)
				if now.After(startedAt) {
					agent.Age = now.Sub(startedAt).Truncate(time.Millisecond)
				}
			}
		}

		session := &report.Sessions[sessionIndex]
		session.Agents = append(session.Agents, agent)
		if agent.Resolved {
			report.ResolvedAgents++
			session.CPUPercent += agent.CPUPercent
			session.RSSBytes += agent.RSSBytes
			session.ProcessCount += agent.ProcessCount
		} else {
			report.UnresolvedAgents++
		}
	}

	for _, session := range report.Sessions {
		report.CPUPercent += session.CPUPercent
		report.RSSBytes += session.RSSBytes
		report.ProcessCount += session.ProcessCount
	}
	if logicalCPUs > 0 {
		report.HostCPUPercent = report.CPUPercent / float64(logicalCPUs)
	}
	return report
}

func indexProcesses(processes []ProcessSample) map[int32]ProcessSample {
	indexed := make(map[int32]ProcessSample, len(processes))
	for _, process := range processes {
		if process.PID > 0 {
			indexed[process.PID] = process
		}
	}
	return indexed
}

func processTree(target AgentTarget, processes map[int32]ProcessSample) map[int32]struct{} {
	owned := make(map[int32]struct{})
	for _, pid := range append([]int32{target.ForegroundProcessGroupID}, target.ForegroundPIDs...) {
		if _, found := processes[pid]; found {
			owned[pid] = struct{}{}
		}
	}
	if len(owned) == 0 {
		return owned
	}

	changed := true
	for changed {
		changed = false
		for pid, process := range processes {
			if _, alreadyOwned := owned[pid]; alreadyOwned {
				continue
			}
			if _, parentOwned := owned[process.PPID]; parentOwned {
				owned[pid] = struct{}{}
				changed = true
			}
		}
	}
	return owned
}

func conflictingOwners(owned []map[int32]struct{}) map[int]bool {
	ownersByPID := make(map[int32][]int)
	for owner, processes := range owned {
		for pid := range processes {
			ownersByPID[pid] = append(ownersByPID[pid], owner)
		}
	}
	conflicts := make(map[int]bool)
	for _, owners := range ownersByPID {
		if len(owners) < 2 {
			continue
		}
		for _, owner := range owners {
			conflicts[owner] = true
		}
	}
	return conflicts
}

func rootPID(target AgentTarget, owned map[int32]struct{}, processes map[int32]ProcessSample) int32 {
	if _, found := owned[target.ForegroundProcessGroupID]; found {
		return target.ForegroundProcessGroupID
	}
	for _, pid := range target.ForegroundPIDs {
		if _, found := owned[pid]; found {
			return pid
		}
	}

	pids := make([]int, 0, len(owned))
	for pid := range owned {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)
	if len(pids) > 0 {
		return int32(pids[0])
	}
	panic(fmt.Sprintf("rootPID called without owned processes: %#v", processes))
}
