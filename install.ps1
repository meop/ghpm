#Requires -Version 5.1
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$GhpmRepo = 'meop/ghpm'
$GhRepo = 'cli/cli'
$GhpmBin = "$env:USERPROFILE\.ghpm\bin"

$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq
  [System.Runtime.InteropServices.Architecture]::Arm64) { 'arm64' } else { 'amd64' }

function Get-LatestRelease($Repo) {
  $url = "https://api.github.com/repos/$Repo/releases/latest"
  Write-Host "  GET $url"
  try {
    Invoke-RestMethod $url
  } catch {
    Write-Error "Failed to fetch release from $Repo : $_"
    exit 1
  }
}

function Find-Asset($Release, $Pattern) {
  $asset = $Release.assets | Where-Object { $_.name -match $Pattern } | Select-Object -First 1
  if (-not $asset) {
    Write-Error "No asset matching '$Pattern' found in $($Release.tag_name)"
    Write-Host "  available assets:" -ForegroundColor Yellow
    $Release.assets | ForEach-Object { Write-Host "    $($_.name)" -ForegroundColor Yellow }
    exit 1
  }
  Write-Host "  matched asset: $($asset.name) -> $($asset.browser_download_url)"
  $asset
}

function Install-Binary($Release, $Pattern, $Binary, $Dest) {
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
  Write-Host "  temp dir: $tmp"
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    $asset = Find-Asset $Release $Pattern
    $zip = Join-Path $tmp 'pkg.zip'
    Write-Host "  downloading to $zip"
    Invoke-WebRequest $asset.browser_download_url -OutFile $zip -UseBasicParsing
    $size = (Get-Item $zip).Length
    Write-Host "  downloaded $size bytes"
    Write-Host "  extracting to $tmp"
    try {
      Expand-Archive $zip -DestinationPath $tmp
    } catch {
      Write-Error "Extraction failed for $zip (${size} bytes): $_"
      exit 1
    }
    if (-not (Test-Path $Dest)) { New-Item -ItemType Directory -Path $Dest | Out-Null }
    $exe = Get-ChildItem $tmp -Recurse -Filter $Binary | Select-Object -First 1
    if (-not $exe) {
      Write-Error "Binary '$Binary' not found in archive at $tmp"
      Write-Host "  archive contents:" -ForegroundColor Yellow
      Get-ChildItem $tmp -Recurse -File | ForEach-Object { Write-Host "    $($_.FullName)" -ForegroundColor Yellow }
      exit 1
    }
    Copy-Item $exe.FullName "$Dest\$Binary" -Force
    Write-Host "  installed $Dest\$Binary"
  } finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

# Install ghpm
Write-Host "Fetching latest ghpm release: github.com/$GhpmRepo"
$GhpmRelease = Get-LatestRelease $GhpmRepo
Write-Host "  version: $($GhpmRelease.tag_name)"
Install-Binary $GhpmRelease "ghpm-.*-windows-$Arch\.zip$" 'ghpm.exe' $GhpmBin

# Install gh (bootstrap — ghpm needs it to operate)
Write-Host "Fetching latest gh release: github.com/$GhRepo"
$GhRelease = Get-LatestRelease $GhRepo
Write-Host "  version: $($GhRelease.tag_name)"
Install-Binary $GhRelease "gh_.*_windows_$Arch\.zip$" 'gh.exe' $GhpmBin
$env:PATH = "$GhpmBin;$env:PATH"
& "$GhpmBin\gh.exe" auth status 2>$null
if ($LASTEXITCODE -ne 0) {
  Write-Host 'Authenticating gh...'
  & "$GhpmBin\gh.exe" auth login
}

Write-Host ''
Write-Host 'To activate ghpm, add ~/.ghpm/bin to PATH and source the env script:'
Write-Host '  nu:     $env.PATH = ($env.PATH | prepend ~/.ghpm/bin); source ~/.ghpm/scripts/env.nu'
Write-Host '  pwsh:   $env:PATH = "$env:USERPROFILE\.ghpm\bin;$env:PATH"; . ~/.ghpm/scripts/env.ps1'
