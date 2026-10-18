// Package host gathers read-only information about the machine the server runs on.
// It reads Linux pseudo-files (/proc, /etc/os-release); on other platforms, or when
// those files are unavailable, the affected fields are left at their zero value.
package host

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Info describes the host.
type Info struct {
	Hostname             string  `json:"hostname"`
	OS                   string  `json:"os"`     // runtime.GOOS
	OSName               string  `json:"osName"` // e.g. "Ubuntu 24.04.1 LTS", may be empty
	Arch                 string  `json:"arch"`
	GoVersion            string  `json:"goVersion"`
	NumCPU               int     `json:"numCpu"`
	UptimeSeconds        uint64  `json:"uptimeSeconds"`
	Load1                float64 `json:"load1"`
	Load5                float64 `json:"load5"`
	Load15               float64 `json:"load15"`
	MemoryTotalBytes     uint64  `json:"memoryTotalBytes"`
	MemoryAvailableBytes uint64  `json:"memoryAvailableBytes"`
}

// Collect assembles the host information. It never fails; unavailable values are
// simply left at zero.
func Collect() Info {
	hostname, _ := os.Hostname()
	memTotal, memAvailable := readMemInfo()
	return Info{
		Hostname:             hostname,
		OS:                   runtime.GOOS,
		OSName:               readOSName(),
		Arch:                 runtime.GOARCH,
		GoVersion:            runtime.Version(),
		NumCPU:               runtime.NumCPU(),
		UptimeSeconds:        readUptime(),
		Load1:                readLoad(0),
		Load5:                readLoad(1),
		Load15:               readLoad(2),
		MemoryTotalBytes:     memTotal,
		MemoryAvailableBytes: memAvailable,
	}
}

func readUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return uint64(seconds)
}

func readLoad(index int) float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if index >= len(fields) {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[index], 64)
	return value
}

// readMemInfo returns total and available /proc/meminfo sizes in bytes, or 0 for
// either when unavailable.
func readMemInfo() (total, available uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = parseKB(fields[1])
		case "MemAvailable:":
			available = parseKB(fields[1])
		}
	}
	return total, available
}

func parseKB(value string) uint64 {
	kb, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

func readOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && key == "PRETTY_NAME" {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return ""
}
