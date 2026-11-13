package api

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var (
	cpuMu      sync.Mutex
	cpuPrev    cpuSample
	cpuPrevSet bool
)

type cpuSample struct {
	idle  uint64
	total uint64
}

func cpuUsage() int {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0
		}
		total += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	cur := cpuSample{idle: idle, total: total}
	if !cpuPrevSet {
		cpuPrev = cur
		cpuPrevSet = true
		return 0
	}
	dTotal := cur.total - cpuPrev.total
	dIdle := cur.idle - cpuPrev.idle
	cpuPrev = cur
	if dTotal == 0 {
		return 0
	}
	return int((dTotal - dIdle) * 100 / dTotal)
}

func memoryUsage() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	if total == 0 {
		return 0
	}
	return int((total - available) * 100 / total)
}

func schedulerLoad() int {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int(v)
}

func diskPartitions() []map[string]any {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil || st.Blocks == 0 {
		return []map[string]any{}
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	usage := 0
	if total > 0 {
		usage = int((total - free) * 100 / total)
	}
	return []map[string]any{{
		"path":     "root",
		"device":   "",
		"usage":    usage,
		"io_util":  0,
		"total_mb": int(total / (1 << 20)),
	}}
}
