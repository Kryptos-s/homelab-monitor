$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $MyInvocation.MyCommand.Path)\..

if (-not (Test-Path dist)) { New-Item -ItemType Directory -Path dist | Out-Null }

$ld="-s -w"

Write-Host "Building dashboard for windows/amd64..."
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -ldflags $ld -o dist/dashboard-windows-amd64.exe ./dashboard

Write-Host "Building dashboard for linux/amd64..."
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags $ld -o dist/dashboard-linux-amd64 ./dashboard
