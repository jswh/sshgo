#!/usr/bin/env pwsh
# sshgo - One-click install script for Windows (PowerShell)
# Usage: irm https://github.com/jswh/sshgo/raw/main/install.ps1 | iex

$Repo = "jswh/sshgo"
$InstallDir = Join-Path $HOME ".local" "bin"

# --- Detect OS and architecture ---
$IsWindows = [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
$OS = if ($IsWindows) { "windows" } else { "unknown" }
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "arm64" }

# On non-Windows (PowerShell Core), detect via uname
if (-not $IsWindows) {
    $unameOs = & uname -s 2>$null
    $unameArch = & uname -m 2>$null
    if ($unameOs) {
        switch ($unameOs.ToString().ToLower()) {
            "linux" { $OS = "linux" }
            "darwin" { $OS = "darwin" }
        }
    }
    if ($unameArch) {
        switch ($unameArch.ToString().ToLower()) {
            "x86_64" { $Arch = "amd64" }
            "amd64" { $Arch = "amd64" }
            "aarch64" { $Arch = "arm64" }
            "arm64" { $Arch = "arm64" }
        }
    }
}

# Windows arm64 is rare but possible
if ($OS -eq "windows" -and $Arch -eq "arm64") {
    # Check if this is actually an ARM64 Windows
    try {
        $armCheck = Get-CimInstance Win32_Processor | Select-Object -First 1
        if ($armCheck.Architecture -eq 5) { # ARM64
            $Arch = "arm64"
        } else {
            $Arch = "amd64" # x64 emulation
        }
    } catch {
        $Arch = "amd64"
    }
}

$BinaryName = "sshgo-${OS}-${Arch}"
if ($OS -eq "windows") {
    $BinaryName += ".exe"
}

Write-Host "Detected: $OS / $Arch"

# --- Fetch latest release tag ---
Write-Host "Fetching latest release information..."
try {
    $Request = [System.Net.WebRequest]::Create("https://github.com/${Repo}/releases/latest")
    $Request.AllowAutoRedirect = $false
    $Response = $Request.GetResponse()
    $Location = $Response.GetResponseHeader("Location")
    $Response.Close()
    
    if (-not $Location) {
        Write-Error "Failed to determine latest release: no redirect location"
        exit 1
    }
    
    $Tag = $Location -replace '.*/tag/', ''
    $Tag = $Tag -replace '/.*', ''  # Remove trailing path
    $Tag = $Tag -replace '\?.*', ''  # Remove query string
    
    if (-not $Tag) {
        Write-Error "Failed to parse tag from redirect: $Location"
        exit 1
    }
} catch {
    Write-Error "Failed to reach GitHub: $_"
    exit 1
}

$DownloadUrl = "https://github.com/${Repo}/releases/download/${Tag}/${BinaryName}"

Write-Host "Latest version: ${Tag}"
Write-Host "Downloading ${BinaryName} ..."

# --- Download ---
$InstallPath = Join-Path $InstallDir "sshgo.exe"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

try {
    $ProgressPreference = 'SilentlyContinue'  # Speed up download
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $InstallPath -UseBasicParsing
    Write-Host "Downloaded to ${InstallPath}"
} catch {
    Write-Error "Download failed: $_"
    exit 1
}

# --- Add to PATH if needed ---
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notlike "*${InstallDir}*") {
    $NewPath = if ($UserPath) { "${UserPath};${InstallDir}" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
    # Update current session
    $env:PATH = "${env:PATH};${InstallDir}"
    Write-Host ""
    Write-Host "Added ${InstallDir} to your PATH (reopen terminal for changes to take effect in new sessions)."
} else {
    Write-Host "${InstallDir} is already in your PATH."
}

Write-Host ""
Write-Host "sshgo ${Tag} installed to ${InstallPath}"
Write-Host "Run 'sshgo --help' to get started."
