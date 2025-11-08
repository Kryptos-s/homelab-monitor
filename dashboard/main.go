package main

import (
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	yaml "gopkg.in/yaml.v3"
)

//go:embed web/static/index.html
var indexHTML string

//go:embed web/static/script.js
var scriptJS string

//go:embed web/static/styles.css
var stylesCSS string

type Alerts struct {
	CPUPct float64 `yaml:"cpu_pct" json:"cpu_pct"`
	MemPct float64 `yaml:"mem_pct" json:"mem_pct"`
	DiskPct float64 `yaml:"disk_pct" json:"disk_pct"`
	TxBps  float64 `yaml:"tx_bps" json:"tx_bps"`
	RxBps  float64 `yaml:"rx_bps" json:"rx_bps"`
}

type Node struct {
	Name   string  `yaml:"name" json:"name"`
	URL    string  `yaml:"url" json:"url"`
	Group  string  `yaml:"group" json:"group"`
	Alerts *Alerts `yaml:"alerts" json:"alerts"`
}

type Config struct {
	ListenAddr     string `yaml:"listen_addr"`
	RefreshSeconds int    `yaml:"refresh_seconds"`
	HistoryLimit   int    `yaml:"history_limit"`
	Nodes          []Node `yaml:"nodes"`
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

type AgentMetrics struct {
	Hostname     string     `json:"hostname"`
	TimestampISO string     `json:"timestamp_iso"`
	CPUPercent   float64    `json:"cpu_percent"`
	MemPercent   float64    `json:"mem_percent"`
	UptimeSec    uint64     `json:"uptime_sec"`
	Disks        []DiskStat `json:"disks"`
	Net          NetStat    `json:"net"`
}

type Sample struct {
	Time         time.Time     `json:"time"`
	Agent        AgentMetrics  `json:"agent"`
	LatencyMs    int64         `json:"latency_ms"` // TCP connect + HTTP fetch
	Error        string        `json:"error,omitempty"`
	Node         Node          `json:"node"`
}

type State struct {
	Config Config
	mu     sync.RWMutex
	// history per node name
	history map[string][]Sample
	latest  map[string]Sample
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{
			ListenAddr:     "0.0.0.0:8080",
			RefreshSeconds: 2,
			HistoryLimit:   500,
		}, nil
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.ListenAddr == "" { c.ListenAddr = "0.0.0.0:8080" }
	if c.RefreshSeconds <= 0 { c.RefreshSeconds = 2 }
	if c.HistoryLimit <= 0 { c.HistoryLimit = 500 }
	return c, nil
}

func fetchOnce(ctx context.Context, n Node) (AgentMetrics, int64, error) {
	start := time.Now()
	u, err := url.Parse(n.URL)
	if err != nil {
		return AgentMetrics{}, 0, err
	}
	// TCP dial latency
	hostPort := u.Host
	if !strings.Contains(hostPort, ":") {
		// default port 80/443? Assume as provided; otherwise TCP dial to 80
		hostPort = hostPort + ":80"
	}
	dialer := net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return AgentMetrics{}, 0, err
	}
	_ = conn.Close()

	// HTTP GET /metrics
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, n.URL+"/metrics", nil)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return AgentMetrics{}, 0, err
	}
	defer resp.Body.Close()
	var am AgentMetrics
	if err := json.NewDecoder(resp.Body).Decode(&am); err != nil {
		return AgentMetrics{}, 0, err
	}
	lat := time.Since(start).Milliseconds()
	return am, lat, nil
}

func (s *State) loop() {
	t := time.NewTicker(time.Duration(s.Config.RefreshSeconds) * time.Second)
	defer t.Stop()
	for {
		s.pollAll()
		<-t.C
	}
}

func (s *State) pollAll() {
	wg := sync.WaitGroup{}
	for _, n := range s.Config.Nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			am, lat, err := fetchOnce(ctx, n)
			s.mu.Lock()
			defer s.mu.Unlock()
			samp := Sample{
				Time:      time.Now(),
				Agent:     am,
				LatencyMs: lat,
				Node:      n,
			}
			if err != nil {
				samp.Error = err.Error()
			}
			if s.history == nil { s.history = map[string][]Sample{} }
			if s.latest == nil { s.latest = map[string]Sample{} }
			h := append(s.history[n.Name], samp)
			if len(h) > s.Config.HistoryLimit {
				h = h[len(h)-s.Config.HistoryLimit:]
			}
			s.history[n.Name] = h
			s.latest[n.Name] = samp
		}()
	}
	wg.Wait()
}

// HTTP handlers

func (s *State) handleIndex(w http.ResponseWriter, r *http.Request) {
	tpl := template.Must(template.New("idx").Parse(indexHTML))
	s.mu.RLock()
	defer s.mu.RUnlock()

	// unique groups
	groups := map[string]struct{}{}
	for _, n := range s.Config.Nodes {
		groups[n.Group] = struct{}{}
	}
	var groupList []string
	for g := range groups { groupList = append(groupList, g) }
	sort.Strings(groupList)

	data := map[string]any{
		"Groups": groupList,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, data)
}

func (s *State) handleStatic(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/static/script.js":
		w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, scriptJS)
	case "/static/styles.css":
		w.Header().Set("Content-Type", "text/css"); fmt.Fprint(w, stylesCSS)
	default:
		http.NotFound(w, r)
	}
}

func (s *State) handleLatest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type Row struct {
		Sample
		Group string `json:"group"`
	}
	var rows []Row
	for _, n := range s.Config.Nodes {
		if samp, ok := s.latest[n.Name]; ok {
			rows = append(rows, Row{Sample: samp, Group: n.Group})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func (s *State) handleHistory(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("node")
	s.mu.RLock()
	defer s.mu.RUnlock()
	if name != "" {
		json.NewEncoder(w).Encode(s.history[name])
		return
	}
	json.NewEncoder(w).Encode(s.history)
}

func (s *State) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=metrics.csv")
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{"time","node","group","latency_ms","cpu_pct","mem_pct","tx_bps","rx_bps","uptime_sec"})
	for _, n := range s.Config.Nodes {
		for _, samp := range s.history[n.Name] {
			_ = cw.Write([]string{
				samp.Time.UTC().Format(time.RFC3339),
				n.Name,
				n.Group,
				fmt.Sprint(samp.LatencyMs),
				fmt.Sprintf("%.2f", samp.Agent.CPUPercent),
				fmt.Sprintf("%.2f", samp.Agent.MemPercent),
				fmt.Sprintf("%.0f", samp.Agent.Net.TxBytesPerSec),
				fmt.Sprintf("%.0f", samp.Agent.Net.RxBytesPerSec),
				fmt.Sprint(samp.Agent.UptimeSec),
			})
		}
	}
}

func (s *State) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.history)
}

func main() {
	cfgPath := "dashboard.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st := &State{Config: cfg, history: map[string][]Sample{}, latest: map[string]Sample{}}

	// first poll to populate
	st.pollAll()
	go st.loop()

	mux := http.NewServeMux()
	mux.HandleFunc("/", st.handleIndex)
	mux.HandleFunc("/static/", st.handleStatic)
	mux.HandleFunc("/api/latest", st.handleLatest)
	mux.HandleFunc("/api/history", st.handleHistory)
	mux.HandleFunc("/export/csv", st.handleExportCSV)
	mux.HandleFunc("/export/json", st.handleExportJSON)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("dashboard listening on http://%s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
