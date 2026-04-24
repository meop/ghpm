#Requires -Version 5.1
[CmdletBinding()]
param(
  [string]$InstallDir = "$env:USERPROFILE\.local\bin"
)

$ErrorActionPreference = 'Stop'

$GhpmRepo = 'meop/ghpm'
$GhRepo   = 'cli/cli'
$GhpmBin  = "$env:USERPROFILE\.ghpm\bin"

$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq
  [System.Runtime.InteropServices.Architecture]::Arm64) { 'arm64' } else { 'amd64' }

function Get-LatestRelease($Repo) {
  Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
}

function Save-Asset($Release, $Pattern, $Dest) {
  $asset = $Release.assets | Where-Object { $_.name -match $Pattern } | Select-Object -First 1
  if (-not $asset) { Write-Error "No asset matching '$Pattern' found in $($Release.tag_name)"; exit 1 }
  Invoke-WebRequest $asset.browser_download_url -OutFile $Dest -UseBasicParsing
}

function Install-Binary($Release, $Pattern, $Binary, $Dest) {
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    $zip = Join-Path $tmp 'pkg.zip'
    Save-Asset $Release $Pattern $zip
    Expand-Archive $zip -DestinationPath $tmp
    if (-not (Test-Path $Dest)) { New-Item -ItemType Directory -Path $Dest | Out-Null }
    $exe = Get-ChildItem $tmp -Recurse -Filter $Binary | Select-Object -First 1
    Copy-Item $exe.FullName $Dest -Force
  } finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

# Install ghpm
Write-Host "Fetching latest ghpm release: github.com/$GhpmRepo"
$GhpmRelease = Get-LatestRelease $GhpmRepo
Install-Binary $GhpmRelease "ghpm-.*-windows-$Arch\.zip$" 'ghpm.exe' $InstallDir
Write-Host "Installed ghpm $($GhpmRelease.tag_name)" -ForegroundColor Green

# Install gh (bootstrap — ghpm needs it to operate)
Write-Host "Fetching latest gh release: github.com/$GhRepo"
$GhRelease = Get-LatestRelease $GhRepo
Install-Binary $GhRelease "gh_.*_windows_$Arch\.zip$" 'gh.exe' $GhpmBin
Write-Host "Installed gh $($GhRelease.tag_name)" -ForegroundColor Green

# Authenticate gh and register it in ghpm manifest
$env:PATH = "$InstallDir;$GhpmBin;$env:PATH"
Write-Host 'Authenticating gh...'
& "$GhpmBin\gh.exe" auth login
Write-Host 'Registering gh in ghpm manifest...'
& "$InstallDir\ghpm.exe" install gh

function Check-EnvPath($Dir) {
  $current = [System.Environment]::GetEnvironmentVariable('Path', 'User')
  if ($current -notlike "*$Dir*") {
    Write-Host "NOTE: $Dir is not in your PATH." -ForegroundColor Yellow
    Write-Host "  Add it with: [System.Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$Dir`", 'User')" -ForegroundColor Yellow
  }
}

Write-Host ''
Check-EnvPath $InstallDir
Check-EnvPath $GhpmBin
