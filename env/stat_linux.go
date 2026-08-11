//go:build linux

package env

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// CPUStat holds aggregated system-level CPU statistics from /proc/stat.
type CPUStat struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
}

// CPUStats holds per-core CPU hits plus system aggregates from /proc/stat,
// along with context-switch and interrupt counts.
type CPUStats struct {
	Total  CPUStat
	PerCPU []CPUStat
	CtxtSW uint64
	Intr   uint64
	NumCPU uint64
}

// GetCPUStats reads /proc/stat and returns CPU statistics.
func GetCPUStats() (*CPUStats, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("read /proc/stat: %w", err)
	}

	out := &CPUStats{}
	cpuRe := regexp.MustCompile(`^cpu[0-9]`)

	for _, line := range bytes.Split(data, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) < 2 {
			continue
		}
		switch {
		case cpuRe.MatchString(fields[0]):
			s := parseCPUStat(fields)
			out.Total.User += s.User
			out.Total.Nice += s.Nice
			out.Total.System += s.System
			out.Total.Idle += s.Idle
			out.Total.IOWait += s.IOWait
			out.Total.IRQ += s.IRQ
			out.Total.SoftIRQ += s.SoftIRQ
			out.PerCPU = append(out.PerCPU, s)
			out.NumCPU++
		case fields[0] == "intr":
			out.Intr, _ = strconv.ParseUint(fields[1], 10, 64)
		case fields[0] == "ctxt":
			out.CtxtSW, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return out, nil
}

func parseCPUStat(fields []string) CPUStat {
	var s CPUStat
	if len(fields) > 8 {
		s.User, _ = strconv.ParseUint(fields[1], 10, 64)
		s.Nice, _ = strconv.ParseUint(fields[2], 10, 64)
		s.System, _ = strconv.ParseUint(fields[3], 10, 64)
		s.Idle, _ = strconv.ParseUint(fields[4], 10, 64)
		s.IOWait, _ = strconv.ParseUint(fields[5], 10, 64)
		s.IRQ, _ = strconv.ParseUint(fields[6], 10, 64)
		s.SoftIRQ, _ = strconv.ParseUint(fields[7], 10, 64)
	}
	return s
}

// MemInfo holds system memory usage from /proc/meminfo (values in bytes).
type MemInfo struct {
	Total  uint64
	Free   uint64
	Used   uint64
	Buffer uint64
	Cached uint64
}

// GetMemInfo reads /proc/meminfo and returns memory statistics.
func GetMemInfo() (*MemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	m := &MemInfo{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		v *= 1024 // kB → bytes
		switch fields[0] {
		case "MemTotal:":
			m.Total = v
		case "MemFree:":
			m.Free = v
		case "Buffers:":
			m.Buffer = v
		case "Cached:":
			m.Cached = v
		}
	}
	m.Used = m.Total - m.Free - m.Buffer - m.Cached
	return m, nil
}

// NetStat holds per-interface network counters from /proc/net/dev.
type NetStat struct {
	RxBytes   uint64
	RxPackets uint64
	TxBytes   uint64
	TxPackets uint64
}

// GetNetStat reads /proc/net/dev and returns network counters for iface.
func GetNetStat(iface string) (*NetStat, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("read /proc/net/dev: %w", err)
	}

	for _, line := range bytes.Split(data, []byte{'\n'}) {
		parts := strings.SplitN(string(line), ":", 2)
		if len(parts) < 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != iface {
			continue
		}
		rest := strings.Fields(parts[1])
		if len(rest) < 10 {
			continue
		}
		s := &NetStat{}
		s.RxBytes, _ = strconv.ParseUint(rest[0], 10, 64)
		s.RxPackets, _ = strconv.ParseUint(rest[1], 10, 64)
		s.TxBytes, _ = strconv.ParseUint(rest[8], 10, 64)
		s.TxPackets, _ = strconv.ParseUint(rest[9], 10, 64)
		return s, nil
	}
	return nil, fmt.Errorf("interface %q not found in /proc/net/dev", iface)
}

// TaskStat holds per-process resource usage from /proc/[pid]/status.
// Values are in bytes for Vm* fields.
type TaskStat struct {
	VmSize   uint64
	VmRSS    uint64
	VolCSW   uint64
	InvolCSW uint64
}

// GetTaskStat reads /proc/[pid]/status and returns process resource usage.
func GetTaskStat(pid int) (*TaskStat, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil, fmt.Errorf("read /proc/%d/status: %w", pid, err)
	}

	t := &TaskStat{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "VmSize:":
			t.VmSize = v * 1024
		case "VmRSS:":
			t.VmRSS = v * 1024
		case "voluntary_ctxt_switches:":
			t.VolCSW = v
		case "nonvoluntary_ctxt_switches:":
			t.InvolCSW = v
		}
	}
	return t, nil
}
