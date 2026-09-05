# Installs the latest (or a pinned) orchestra release for Windows.
#
# Usage:
#   irm https://raw.githubusercontent.com/GreyKxtx/Orchestra/master/scripts/install.ps1 | iex
#   $env:VERSION = "v0.3.0"; .\install.ps1
#
# Downloads orchestra_<version>_windows-amd64.zip from GitHub Releases,
# verifies it against the .sha256 published alongside it (release.yml writes
# both), and installs orchestra.exe.
[CmdletBinding()]
param(
    [string]$Version = $env:VERSION,
    [string]$InstallDir = $(if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Orchestra\bin" })
)

$ErrorActionPreference = "Stop"
$repo = "GreyKxtx/Orchestra"
$target = "windows-amd64"

if (-not $Version) {
    Write-Host "Resolving latest release..."
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
    $Version = $release.tag_name
    if (-not $Version) {
        throw "install.ps1: could not resolve the latest release - pass -Version v0.x.y explicitly"
    }
}

$name = "orchestra_${Version}_${target}"
$archive = "$name.zip"
$baseUrl = "https://github.com/$repo/releases/download/$Version"

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $workDir -Force | Out-Null
try {
    $archivePath = Join-Path $workDir $archive
    $shaPath = "$archivePath.sha256"

    Write-Host "Downloading $archive ($Version)..."
    Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile $archivePath
    Invoke-WebRequest -Uri "$baseUrl/$archive.sha256" -OutFile $shaPath

    Write-Host "Verifying checksum..."
    $expected = (Get-Content $shaPath -Raw).Trim().Split(" ")[0].Trim()
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
    if ($expected.ToLower() -ne $actual) {
        throw "checksum mismatch: expected $expected, got $actual"
    }

    Write-Host "Installing to $InstallDir..."
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $workDir -Force
    Copy-Item -Path (Join-Path $workDir "orchestra.exe") -Destination (Join-Path $InstallDir "orchestra.exe") -Force

    $exe = Join-Path $InstallDir "orchestra.exe"
    Write-Host "Installed $(& $exe version)"

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not ($userPath -split ";" | Where-Object { $_ -eq $InstallDir })) {
        Write-Host "Note: $InstallDir is not on your PATH. Add it, e.g.:"
        Write-Host "  setx PATH `"$InstallDir;%PATH%`""
    }
} finally {
    Remove-Item -Recurse -Force $workDir -ErrorAction SilentlyContinue
}
