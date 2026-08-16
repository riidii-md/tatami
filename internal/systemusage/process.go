package systemusage

import (
	"fmt"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

func snapshotProcesses() ([]ProcessSample, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("list local processes: %w", err)
	}

	samples := make([]ProcessSample, 0, len(processes))
	for _, candidate := range processes {
		ppid, err := candidate.Ppid()
		if err != nil {
			continue
		}
		startedAt, err := candidate.CreateTime()
		if err != nil {
			continue
		}
		times, err := candidate.Times()
		if err != nil {
			continue
		}
		memory, err := candidate.MemoryInfo()
		if err != nil {
			continue
		}
		samples = append(samples, ProcessSample{
			PID:             candidate.Pid,
			PPID:            ppid,
			StartedAtMillis: startedAt,
			CPUSeconds:      times.User + times.System,
			RSSBytes:        memory.RSS,
		})
	}
	return samples, nil
}

func totalMemoryBytes() (uint64, error) {
	memory, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return memory.Total, nil
}
