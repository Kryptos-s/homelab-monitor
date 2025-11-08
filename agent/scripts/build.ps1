$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $MyInvocation.MyCommand.Path)\..

if (-not (Test-Path dist)) { New-Item -ItemType Directory -Path dist | Out-Null }

$env:GOFLAGS="-trimpath"
$ld="-s -w"

Write-Host "Building agent for windows/amd64..."
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -ldflags $ld -o dist/agent-windows-amd64.exe ./agent

Write-Host "Building agent for linux/amd64..."
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags $ld -o dist/agent-linux-amd64 ./agent
