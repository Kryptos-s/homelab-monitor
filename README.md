# Homelab Monitor (Go, offline)
Minimal self-hosted monitor for LAN nodes. One agent per node. One dashboard. No cloud.

## Features
- Agent → GET /metrics (CPU %, RAM %, disk %, uptime, Tx/Rx B/s)
- Dashboard → polls agents, groups nodes, highlights alerts
- Works offline. Single static binaries. CORS enabled
- YAML config. Bounded in-memory history. CSV/JSON export
- Cross-platform: Linux and Windows
- Small binaries (-ldflags "-s -w", -trimpath)

## Build
```bash
./agent/scripts/build.sh
./dashboard/scripts/build.sh
```
PowerShell:
```powershell
./agent/scripts/build.ps1
./dashboard/scripts/build.ps1
```

## Run
Edit `dashboard/dashboard.yaml`, then:
```bash
./dist/dashboard-linux-amd64 dashboard/dashboard.yaml
```
