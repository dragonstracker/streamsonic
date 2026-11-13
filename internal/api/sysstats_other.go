//go:build !linux

package api

func cpuUsage() int                    { return 0 }
func memoryUsage() int                 { return 0 }
func schedulerLoad() int               { return 0 }
func diskPartitions() []map[string]any { return []map[string]any{} }
