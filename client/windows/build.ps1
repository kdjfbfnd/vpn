$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$repo = Split-Path -Parent $root
$go = Join-Path $repo ".local-tools\go\bin\go.exe"
$gofmt = Join-Path $repo ".local-tools\go\bin\gofmt.exe"
if (!(Test-Path $go)) {
    throw "Go was not found at $go"
}
if (!(Test-Path $gofmt)) {
    throw "gofmt was not found at $gofmt"
}

$env:GOCACHE = Join-Path $repo ".local-tools\go-cache"
$env:GOPATH = Join-Path $repo ".local-tools\gopath"
New-Item -ItemType Directory -Force $env:GOCACHE, $env:GOPATH | Out-Null

Push-Location $PSScriptRoot
try {
    $goFiles = Get-ChildItem -LiteralPath $PSScriptRoot -Filter *.go | ForEach-Object { $_.FullName }
    & $gofmt -w @goFiles
    if ($LASTEXITCODE -ne 0) { throw "gofmt failed with exit code $LASTEXITCODE" }
    & $go mod tidy
    if ($LASTEXITCODE -ne 0) { throw "go mod tidy failed with exit code $LASTEXITCODE" }
    & $go build -trimpath -ldflags "-s -w -H windowsgui" -o (Join-Path $PSScriptRoot "solovpn-client.exe") .
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
}
finally {
    Pop-Location
}

Write-Host "Built: $(Join-Path $PSScriptRoot 'solovpn-client.exe')"
