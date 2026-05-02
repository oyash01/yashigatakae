# yashigatakae installer (Windows / PowerShell)
#
# Usage:
#   irm https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.ps1 | iex
#
# Env overrides:
#   $env:YASHI_VERSION = "v0.1.0"
#   $env:YASHI_PREFIX  = "$env:LOCALAPPDATA\yashigatakae"
#   $env:YASHI_NO_INIT = "1"

$ErrorActionPreference = 'Stop'

$Repo    = 'oyash01/yashigatakae'
$Version = if ($env:YASHI_VERSION) { $env:YASHI_VERSION } else { 'latest' }
$Prefix  = if ($env:YASHI_PREFIX)  { $env:YASHI_PREFIX  } else { Join-Path $env:LOCALAPPDATA 'yashigatakae' }

function Info($m) { Write-Host "  · $m" }
function Ok($m)   { Write-Host "✓ $m" -ForegroundColor Green }
function Fail($m) { Write-Host "✗ $m" -ForegroundColor Red; exit 1 }

# ── detect arch ────────────────────────────────────────────────────
$arch = if ([Environment]::Is64BitOperatingSystem) { 'amd64' } else { Fail 'unsupported arch (need amd64)' }
$asset = "yashigatakae-windows-$arch.zip"

# ── resolve version ────────────────────────────────────────────────
if ($Version -eq 'latest') {
  Info 'resolving latest release...'
  $api = "https://api.github.com/repos/$Repo/releases/latest"
  try {
    $rel = Invoke-RestMethod -UseBasicParsing -Uri $api
    $Version = $rel.tag_name
  } catch {
    Fail 'no published release yet — set $env:YASHI_VERSION explicitly or build from source: `go install github.com/oyash01/yashigatakae/cmd/yashigatakae@latest`'
  }
}
Info "version: $Version"
Info "asset:   $asset"

# ── download ───────────────────────────────────────────────────────
$url = "https://github.com/$Repo/releases/download/$Version/$asset"
$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "yashi-$([guid]::NewGuid())")
try {
  Info "downloading $url"
  $zip = Join-Path $tmp $asset
  Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $zip

  Info 'extracting'
  Expand-Archive -Path $zip -DestinationPath $tmp -Force

  $bin = Get-ChildItem -Path $tmp -Filter 'yashigatakae.exe' -Recurse | Select-Object -First 1
  if (-not $bin) { Fail 'binary missing from archive' }

  if (-not (Test-Path $Prefix)) { New-Item -ItemType Directory -Path $Prefix -Force | Out-Null }
  $dest = Join-Path $Prefix 'yashigatakae.exe'
  Copy-Item -Path $bin.FullName -Destination $dest -Force
  Ok "installed to $dest"

  # ── PATH check ───────────────────────────────────────────────────
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$Prefix*") {
    Info "adding $Prefix to user PATH"
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$Prefix", 'User')
    $env:Path = "$env:Path;$Prefix"
  }

  # ── sanity check ─────────────────────────────────────────────────
  & $dest --version

  # ── auto-init ────────────────────────────────────────────────────
  if ($env:YASHI_NO_INIT -ne '1') {
    Info 'running yashigatakae init'
    & $dest init
  }
  Ok 'done — try: yashigatakae doctor'
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
