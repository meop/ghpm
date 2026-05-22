[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$GhRepo = 'cli/cli'
$GhpmRepo = 'meop/ghpm'
$SheeshRepo = 'meop/sheesh'
$GhpmBin = "$env:USERPROFILE\.ghpm\bin"
$GhpmShim = "$env:USERPROFILE\.ghpm\shim"

$IsArm64 = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq `
  [System.Runtime.InteropServices.Architecture]::Arm64
$Arch = if ($IsArm64) { 'aarch64' } else { 'x86_64' }
$GoArch = if ($IsArm64) { 'arm64' } else { 'amd64' }

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

function Install-AllExe($Release, $Pattern, $Dest) {
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
    Expand-Archive $zip -DestinationPath $tmp
    if (-not (Test-Path $Dest)) { New-Item -ItemType Directory -Path $Dest | Out-Null }
    Get-ChildItem $tmp -Recurse -Filter '*.exe' | ForEach-Object {
      Copy-Item $_.FullName "$Dest\$($_.Name)" -Force
      Write-Host "  installed $Dest\$($_.Name)"
    }
  } finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
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

# Install gh
Write-Host "Fetching latest gh release: github.com/$GhRepo"
$GhRelease = Get-LatestRelease $GhRepo
Write-Host "  version: $($GhRelease.tag_name)"
Install-Binary $GhRelease "gh_.*_windows_$GoArch\.zip$" 'gh.exe' $GhpmBin
$env:PATH = "$GhpmBin;$env:PATH"
& "$GhpmBin\gh.exe" auth status 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) {
  Write-Host 'Authenticating gh...'
  & "$GhpmBin\gh.exe" auth login --insecure-storage
}

# Install ghpm
Write-Host "Fetching latest ghpm release: github.com/$GhpmRepo"
$GhpmRelease = Get-LatestRelease $GhpmRepo
Write-Host "  version: $($GhpmRelease.tag_name)"
Install-Binary $GhpmRelease "ghpm-.*-windows-$GoArch\.zip$" 'ghpm.exe' $GhpmBin

# Install shim (sheesh runtime + kebab stamper)
Write-Host "Fetching latest shim release: github.com/$SheeshRepo"
$SheeshRelease = Get-LatestRelease $SheeshRepo
Write-Host "  version: $($SheeshRelease.tag_name)"
Install-AllExe $SheeshRelease "sheesh-.*-windows-$Arch\.zip$" $GhpmShim

Write-Host ''
Write-Host 'Refreshing repo sources...'
& "$GhpmBin\ghpm.exe" refresh

Write-Host ''
Write-Host 'Refer to the project README for how to activate ghpm in your shell.'
