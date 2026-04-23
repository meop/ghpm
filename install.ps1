#Requires -Version 5.1
[CmdletBinding()]
param(
  [string]$InstallDir = "$env:USERPROFILE\.local\bin"
)

$ErrorActionPreference = "Stop"

$GhpmRepo = "meop/ghpm"
$GhRepo = "cli/cli"
$GhpmBin = "$env:USERPROFILE\.ghpm\bin"

# Detect architecture
$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) {
  "arm64"
} else {
  "x86_64"
}

function Get-LatestRelease($Repo) {
  Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
}

function Save-Asset($Release, $Pattern, $Dest) {
  $asset = $Release.assets | Where-Object { $_.name -match $Pattern } | Select-Object -First 1
  if (-not $asset) {
    Write-Error "No asset matching '$Pattern' found in $($Release.tag_name)"
    exit 1
  }
  Invoke-WebRequest $asset.browser_download_url -OutFile $Dest -UseBasicParsing
}

# --- Install ghpm ---
Write-Host "Fetching latest ghpm release..."
$GhpmRelease = Get-LatestRelease $GhpmRepo
$GhpmPattern = "ghpm-.*-windows-$Arch\.zip"

$TmpDir = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TmpDir | Out-Null

try {
  $ZipPath = Join-Path $TmpDir "ghpm.zip"
  Save-Asset $GhpmRelease $GhpmPattern $ZipPath
  Expand-Archive $ZipPath -DestinationPath $TmpDir

  if (-not (Test-Path $InstallDir)) { New-Item -ItemType Directory -Path $InstallDir | Out-Null }
  Copy-Item (Join-Path $TmpDir "ghpm.exe") $InstallDir -Force
  Write-Host "Installed ghpm $($GhpmRelease.tag_name) to $InstallDir\ghpm.exe" -ForegroundColor Green
} finally {
  Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

# --- Check / install gh CLI ---
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  $answer = Read-Host "gh CLI not found. Install it from its GitHub release now? [y/N]"
  if ($answer -match "^[yY]$") {
    $GhRelease = Get-LatestRelease $GhRepo
    $GhPattern = "gh_.*_windows_$Arch\.zip"

    $GhTmp = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $GhTmp | Out-Null

    try {
      $GhZip = Join-Path $GhTmp "gh.zip"
      Save-Asset $GhRelease $GhPattern $GhZip
      Expand-Archive $GhZip -DestinationPath $GhTmp

      if (-not (Test-Path $GhpmBin)) { New-Item -ItemType Directory -Path $GhpmBin | Out-Null }
      $GhExe = Get-ChildItem $GhTmp -Recurse -Filter "gh.exe" | Select-Object -First 1
      Copy-Item $GhExe.FullName $GhpmBin -Force
    } finally {
      Remove-Item $GhTmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    Write-Host "Installed gh to $GhpmBin\gh.exe" -ForegroundColor Green
    Write-Host "Registering gh in ghpm manifest..."
    & "$InstallDir\ghpm.exe" install gh
  }
}

# --- Remind about PATH ---
Write-Host ""
$CurrentPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
if ($CurrentPath -notlike "*$InstallDir*") {
    Write-Host "NOTE: $InstallDir is not in your PATH." -ForegroundColor Yellow
    Write-Host "  Add it with:  [System.Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$InstallDir`", 'User')" -ForegroundColor Yellow
}
if ($CurrentPath -notlike "*$GhpmBin*") {
    Write-Host "NOTE: $GhpmBin is not in your PATH." -ForegroundColor Yellow
    Write-Host "  Add it with:  [System.Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$GhpmBin`", 'User')" -ForegroundColor Yellow
}
