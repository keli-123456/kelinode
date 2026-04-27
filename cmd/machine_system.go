package cmd

import (
	"bufio"
	"bytes"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type machineSystemSampler struct {
	mu          sync.Mutex
	cpu         machineCPUSample
	hasCPU      bool
	network     machineNetworkSample
	hasNetwork  bool
	cachedIP    map[string]any
	cachedIPAt  time.Time
	hostname    string
	hostnameSet bool
}

type machineCPUSample struct {
	total uint64
	idle  uint64
}

type machineNetworkSample struct {
	at      time.Time
	rxBytes uint64
	txBytes uint64
}

func newMachineSystemSampler() *machineSystemSampler {
	return &machineSystemSampler{}
}

func (s *machineSystemSampler) Snapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	cpuPercent := s.sampleCPUPercent()
	memStatus, swapStatus := readMachineMemoryStatus()
	diskStatus := readMachineDiskStatus()
	networkStatus := s.sampleNetworkStatus(now)
	ipStatus := s.sampleIPStatus(now)
	uptime := readMachineUptimeSeconds()
	hostname := s.machineHostname()

	system := map[string]any{
		"hostname":     hostname,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"ip":           ipStatus,
		"collected_at": now.Unix(),
	}

	return map[string]any{
		"cpu":    cpuPercent,
		"mem":    memStatus,
		"swap":   swapStatus,
		"disk":   diskStatus,
		"net":    networkStatus,
		"ip":     ipStatus,
		"system": system,
		"uptime": uptime,
	}
}

func (s *machineSystemSampler) sampleCPUPercent() float64 {
	current, ok := readMachineCPUSample()
	if !ok {
		return 0
	}
	if !s.hasCPU {
		s.cpu = current
		s.hasCPU = true
		return 0
	}

	totalDelta := current.total - s.cpu.total
	idleDelta := current.idle - s.cpu.idle
	s.cpu = current
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
}

func (s *machineSystemSampler) sampleNetworkStatus(now time.Time) map[string]any {
	current := machineNetworkSample{at: now}
	current.rxBytes, current.txBytes = readMachineNetworkBytes()

	rxRate := float64(0)
	txRate := float64(0)
	if s.hasNetwork {
		elapsed := current.at.Sub(s.network.at).Seconds()
		if elapsed > 0 {
			if current.rxBytes >= s.network.rxBytes {
				rxRate = float64(current.rxBytes-s.network.rxBytes) / elapsed
			}
			if current.txBytes >= s.network.txBytes {
				txRate = float64(current.txBytes-s.network.txBytes) / elapsed
			}
		}
	}
	s.network = current
	s.hasNetwork = true

	return map[string]any{
		"rx_bytes": current.rxBytes,
		"tx_bytes": current.txBytes,
		"rx_rate":  rxRate,
		"tx_rate":  txRate,
		"rx_bps":   rxRate,
		"tx_bps":   txRate,
	}
}

func (s *machineSystemSampler) sampleIPStatus(now time.Time) map[string]any {
	if s.cachedIP != nil && now.Sub(s.cachedIPAt) < 10*time.Minute {
		return s.cachedIP
	}
	s.cachedIP = readMachineIPStatus(s.machineHostname())
	s.cachedIPAt = now
	return s.cachedIP
}

func (s *machineSystemSampler) machineHostname() string {
	if s.hostnameSet {
		return s.hostname
	}
	s.hostname, _ = os.Hostname()
	s.hostnameSet = true
	return s.hostname
}

func readMachineCPUSample() (machineCPUSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return machineCPUSample{}, false
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		return machineCPUSample{}, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return machineCPUSample{}, false
	}

	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return machineCPUSample{}, false
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return machineCPUSample{total: total, idle: idle}, true
}

func readMachineMemoryStatus() (map[string]any, map[string]any) {
	values := readMachineMeminfo()
	memTotal := values["MemTotal"] * 1024
	memAvailable := values["MemAvailable"] * 1024
	if memAvailable == 0 {
		memAvailable = (values["MemFree"] + values["Buffers"] + values["Cached"]) * 1024
	}
	swapTotal := values["SwapTotal"] * 1024
	swapFree := values["SwapFree"] * 1024

	return map[string]any{
			"total": memTotal,
			"used":  saturatingSub(memTotal, memAvailable),
		}, map[string]any{
			"total": swapTotal,
			"used":  saturatingSub(swapTotal, swapFree),
		}
}

func readMachineMeminfo() map[string]uint64 {
	out := map[string]uint64{}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return out
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out[key] = value
	}
	return out
}

func readMachineDiskStatus() map[string]any {
	output, err := exec.Command("df", "-B1", "-P", "/").Output()
	if err != nil {
		return map[string]any{"total": uint64(0), "used": uint64(0)}
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return map[string]any{"total": uint64(0), "used": uint64(0)}
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 3 {
		return map[string]any{"total": uint64(0), "used": uint64(0)}
	}
	total, _ := strconv.ParseUint(fields[1], 10, 64)
	used, _ := strconv.ParseUint(fields[2], 10, 64)
	return map[string]any{"total": total, "used": used}
}

func readMachineNetworkBytes() (uint64, uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}

	var rxBytes uint64
	var txBytes uint64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		name, payload, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		iface := strings.TrimSpace(name)
		if iface == "" || iface == "lo" {
			continue
		}
		fields := strings.Fields(payload)
		if len(fields) < 16 {
			continue
		}
		rx, errRx := strconv.ParseUint(fields[0], 10, 64)
		tx, errTx := strconv.ParseUint(fields[8], 10, 64)
		if errRx != nil || errTx != nil {
			continue
		}
		rxBytes += rx
		txBytes += tx
	}
	return rxBytes, txBytes
}

func readMachineUptimeSeconds() uint64 {
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

func readMachineIPStatus(hostname string) map[string]any {
	local := make([]string, 0)
	publicIPv4 := ""
	publicIPv6 := ""

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipText := machineAddrIP(addr)
				if ipText == "" {
					continue
				}
				local = append(local, ipText)
				parsed, err := netip.ParseAddr(ipText)
				if err != nil || !parsed.IsGlobalUnicast() || parsed.IsPrivate() {
					continue
				}
				if parsed.Is4() && publicIPv4 == "" {
					publicIPv4 = ipText
				}
				if parsed.Is6() && publicIPv6 == "" {
					publicIPv6 = ipText
				}
			}
		}
	}

	return map[string]any{
		"hostname":    hostname,
		"public_ipv4": publicIPv4,
		"public_ipv6": publicIPv6,
		"local":       local,
	}
}

func machineAddrIP(addr net.Addr) string {
	switch typed := addr.(type) {
	case *net.IPNet:
		return typed.IP.String()
	case *net.IPAddr:
		return typed.IP.String()
	default:
		return ""
	}
}

func saturatingSub(total uint64, free uint64) uint64 {
	if total < free {
		return 0
	}
	return total - free
}
