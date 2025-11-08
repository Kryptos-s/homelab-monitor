package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gnet "github.com/shirou/gopsutil/v3/net"
	yaml "gopkg.in/yaml.v3"
)


type Config struct {
	ListenAddr           string `yaml:"listen_addr"`
	HostnameOverride     string `yaml:"hostname_override"`
	RefreshWindowSeconds int    `yaml:"refresh_window_seconds"`
}

type DiskStat struct {
	Mountpoint string  `json:"mountpoint"`
	UsedPct    float64 `json:"used_pct"`
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
}

type NetStat struct {
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
}

type Metrics struct {
	Hostname     string     `json:"hostname"`
	TimestampISO string     `json:"timestamp_iso"`
	CPUPercent   float64    `json:"cpu_percent"`
	MemPercent   float64    `json:"mem_percent"`
	UptimeSec    uint64     `json:"uptime_sec"`
	Disks        []DiskStat `json:"disks"`
	Net          NetStat    `json:"net"`
}

var (
	cfg            Config
	lastTx, lastRx uint64
	lastTime       time.Time
	mu             sync.Mutex
)

func loadConfig(path string) (Config, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		// default
		return Config{
			ListenAddr:           "0.0.0.0:9876",
			RefreshWindowSeconds: 2,
		}, nil
	}
	var c Config
	if err := yaml.Unmarshal(d, &c); err != nil {
		return c, err
	}
	if c.ListenAddr == "" {
		c.ListenAddr = "0.0.0.0:9876"
	}
	if c.RefreshWindowSeconds <= 0 {
		c.RefreshWindowSeconds = 2
	}
	return c, nil
}

func getHostname(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func sampleNet() (tx, rx uint64) {
	counters, _ := gnet.IOCounters(true)
	var totalTx, totalRx uint64
	for _, c := range counters {
		// skip loopback
		if c.Name == "lo" || strings.HasPrefix(strings.ToLower(c.Name), "loopback") {
			continue
		}
		totalTx += c.BytesSent
		totalRx += c.BytesRecv
	}
	return totalTx, totalRx
}

func computeNet(window time.Duration) NetStat {
	mu.Lock()
	defer mu.Unlock()

	now := time.Now()
	tx, rx := sampleNet()
	if lastTime.IsZero() {
		lastTime = now
		lastTx, lastRx = tx, rx
		return NetStat{TxBytesPerSec: 0, RxBytesPerSec: 0}
	}
	// If last sample is older than window, take a fresh sleep-based window for stability
	elapsed := now.Sub(lastTime).Seconds()
	if elapsed < 0.5 {
		// too soon; wait a bit to avoid spikes
		time.Sleep(200 * time.Millisecond)
		now = time.Now()
		tx, rx = sampleNet()
		elapsed = now.Sub(lastTime).Seconds()
	}
	if elapsed <= 0 {
		elapsed = 1
	}
	dTx := float64(tx - lastTx)
	dRx := float64(rx - lastRx)
	lastTx, lastRx = tx, rx
	lastTime = now
	return NetStat{
		TxBytesPerSec: dTx / elapsed,
		RxBytesPerSec: dRx / elapsed,
	}
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	// CORS for browser-based dashboards
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	hostname := getHostname(cfg.HostnameOverride)

	// CPU (%)
	cpuPercents, _ := cpu.Percent(300*time.Millisecond, false)
	var cpuPct float64
	if len(cpuPercents) > 0 {
		cpuPct = cpuPercents[0]
	}

	// Memory
	vm, _ := mem.VirtualMemory()

	// Disks (only mountpoints with filesystem and non-virtual)
	partitions, _ := disk.Partitions(true)
	disks := make([]DiskStat, 0, len(partitions))
	seen := map[string]bool{}
	for _, p := range partitions {
		mp := p.Mountpoint
		if mp == "" || seen[mp] {
			continue
		}
		seen[mp] = true
		usage, err := disk.Usage(mp)
		if err != nil {
			continue
		}
		disks = append(disks, DiskStat{
			Mountpoint: mp,
			UsedPct:    usage.UsedPercent,
			TotalBytes: usage.Total,
			UsedBytes:  usage.Used,
		})
	}

	// Uptime
	up, _ := host.Uptime()

	// Net throughput
	netStat := computeNet(time.Duration(cfg.RefreshWindowSeconds) * time.Second)

	m := Metrics{
		Hostname:     hostname,
		TimestampISO: time.Now().UTC().Format(time.RFC3339),
		CPUPercent:   cpuPct,
		MemPercent:   vm.UsedPercent,
		UptimeSec:    up,
		Disks:        disks,
		Net:          netStat,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "agent.yaml", "Path to agent YAML config")
	flag.Parse()

	c, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	cfg = c

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metricsHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("agent listening on http://%s", cfg.ListenAddr)
	l, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	if err := server.Serve(l); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
