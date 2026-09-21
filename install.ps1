# aibris installer for Windows
# Install verified aibris.exe from GitHub Releases into a user prefix.
# No admin or Bash required.

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Version = "latest",

    [Parameter()]
    [string]$Prefix = "",

    [Parameter()]
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "",

    [Parameter()]
    [switch]$AddToPath,

    [Parameter()]
    [switch]$Help
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$script:Repo = "sungjunlee/aibris"
$script:Binary = "aibris.exe"
$script:TempRoot = $null

function Write-Log {
    param([string]$Message)
    Write-Host $Message
}

function Write-Error-Message {
    param([string]$Message)
    Write-Error $Message
}

function Show-Usage {
    @"
Install aibris.

Usage:
  irm https://raw.githubusercontent.com/$script:Repo/refs/heads/main/install.ps1 | iex
  irm https://raw.githubusercontent.com/$script:Repo/refs/heads/main/install.ps1 | iex; Install-Aibris
  irm https://raw.githubusercontent.com/$script:Repo/refs/heads/main/install.ps1 | iex; Install-Aibris -Version 0.12.1
  .\install.ps1
  .\install.ps1 -Version latest
  .\install.ps1 -Version 0.12.1
  .\install.ps1 -Prefix "C:\tools\bin"
  .\install.ps1 -AddToPath

Options:
  -Version <string>    Install version (default: latest)
                       - "latest": latest GitHub Release
                       - X.Y.Z or vX.Y.Z: specific version
  -Prefix <path>       Install directory (default: $env:LOCALAPPDATA\Programs\aibris)
  -Arch <string>       Architecture: amd64 or arm64 (default: auto-detect)
  -AddToPath           Add install directory to user PATH
  -Help                Show this help

Examples:
  .\install.ps1
  .\install.ps1 -Version 0.12.1
  .\install.ps1 -Prefix "$env:USERPROFILE\bin"
  .\install.ps1 -AddToPath
"@
}

function Get-DefaultInstallDir {
    if (-not $env:LOCALAPPDATA) {
        Write-Error-Message "LOCALAPPDATA is not set"
        exit 1
    }
    Join-Path $env:LOCALAPPDATA "Programs\aibris"
}

function Invoke-Cleanup {
    if ($script:TempRoot -and (Test-Path $script:TempRoot)) {
        Remove-Item -Recurse -Force $script:TempRoot -ErrorAction SilentlyContinue
    }
}

function Get-Architecture {
    if ($Arch) {
        return $Arch
    }

    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default {
            Write-Error-Message "Unsupported architecture: $arch"
            exit 1
        }
    }
}

function Normalize-Version {
    param([string]$Ver)
    if ($Ver -match '^\d') {
        return "v$Ver"
    }
    return $Ver
}

function Get-LatestReleaseTag {
    $url = "https://api.github.com/repos/$script:Repo/releases/latest"
    try {
        $response = Invoke-RestMethod -Uri $url -UseBasicParsing -TimeoutSec 10
        return $response.tag_name
    }
    catch {
        Write-Error-Message "Could not resolve latest GitHub Release: $_"
        exit 1
    }
}

function Get-Sha256 {
    param([string]$Path)
    $hash = Get-FileHash -Path $Path -Algorithm SHA256
    return $hash.Hash
}

function Install-Binary {
    param(
        [string]$Source,
        [string]$Destination
    )

    Write-Log "Installing $script:Binary to $Destination"

    $destDir = Split-Path -Parent $Destination
    if (-not (Test-Path $destDir)) {
        New-Item -ItemType Directory -Path $destDir -Force | Out-Null
    }

    # Stage-then-replace: if destination exists and is locked, preserve it
    if (Test-Path $Destination) {
        try {
            # Test if file is locked by trying to open it exclusively
            $fileStream = [System.IO.File]::Open($Destination, 'Open', 'Read', 'None')
            $fileStream.Close()
            # File is not locked, proceed with replacement
        }
        catch {
            Write-Error-Message "Existing $script:Binary is locked or in use. Close any running instances and try again."
            exit 1
        }
    }

    Copy-Item -Path $Source -Destination $Destination -Force
    Write-Log "Installed $script:Binary to $Destination"
}

function Install-Release {
    param([string]$Tag)

    $arch = Get-Architecture
    $asset = "aibris_windows_${arch}.zip"
    $url = "https://github.com/$script:Repo/releases/download/$Tag/$asset"
    $checksumsUrl = "https://github.com/$script:Repo/releases/download/$Tag/checksums.txt"

    $script:TempRoot = New-TemporaryFile | ForEach-Object { Remove-Item $_; New-Item -ItemType Directory -Path $_ -Force } | Select-Object -ExpandProperty FullName
    $archivePath = Join-Path $script:TempRoot $asset
    $checksumsPath = Join-Path $script:TempRoot "checksums.txt"
    $extractDir = Join-Path $script:TempRoot "extract"

    Write-Log "Downloading $asset..."
    try {
        Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing -TimeoutSec 120
        Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath -UseBasicParsing -TimeoutSec 30
    }
    catch {
        Write-Error-Message "Failed to download release: $_"
        exit 1
    }

    # Verify checksum
    $checksums = Get-Content $checksumsPath
    $expectedLine = $checksums | Where-Object { $_ -match "\s+$([regex]::Escape($asset))$" }
    if (-not $expectedLine) {
        Write-Error-Message "Checksum for $asset not found in checksums.txt"
        exit 1
    }

    $expected = ($expectedLine -split '\s+')[0]
    $actual = Get-Sha256 -Path $archivePath

    if ($actual -ne $expected) {
        Write-Error-Message "SHA-256 checksum mismatch for ${asset}:`nExpected: $expected`nActual:   $actual"
        exit 1
    }

    Write-Log "Checksum verified: $expected"

    # Extract
    New-Item -ItemType Directory -Path $extractDir -Force | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $binaryPath = Get-ChildItem -Path $extractDir -Recurse -Filter $script:Binary | Select-Object -First 1 -ExpandProperty FullName
    if (-not $binaryPath) {
        Write-Error-Message "$script:Binary not found in archive"
        exit 1
    }

    return $binaryPath
}

function Test-PathContainsDir {
    param([string]$Dir)
    $pathDirs = $env:Path -split ';' | Where-Object { $_ }
    return $pathDirs -contains $Dir
}

function Add-ToUserPath {
    param([string]$Dir)

    if (Test-PathContainsDir -Dir $Dir) {
        Write-Log "$Dir is already on PATH"
        return
    }

    Write-Log "Adding $Dir to user PATH..."
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $Dir) {
        $newPath = ($entries + $Dir) -join ';'
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$Dir;$env:Path"
        Write-Log "Added $Dir to user PATH"
    }
}

function Show-PathHint {
    param([string]$InstallDir)

    if (Test-PathContainsDir -Dir $InstallDir) {
        return
    }

    Write-Host ""
    Write-Host "$script:Binary was installed to $InstallDir, but that directory is not on your PATH yet."
    Write-Host ""
    Write-Host "Add it for future PowerShell sessions:"
    Write-Host '  $dir = "' + $InstallDir + '"'
    Write-Host '  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")'
    Write-Host '  $entries = @($userPath -split '';'' | Where-Object { $_ })'
    Write-Host '  if ($entries -notcontains $dir) {'
    Write-Host '      [Environment]::SetEnvironmentVariable('
    Write-Host '          "Path",'
    Write-Host '          (($entries + $dir) -join '';''),'
    Write-Host '          "User"'
    Write-Host '      )'
    Write-Host '  }'
    Write-Host ""
    Write-Host "Use it in this shell now:"
    Write-Host "  `$env:Path = `"$InstallDir;`$env:Path`""
    Write-Host "  aibris.exe --version"
    Write-Host ""
}

function Install-Aibris {
    [CmdletBinding()]
    param(
        [string]$Version = "latest",
        [string]$Prefix = "",
        [string]$Arch = "",
        [switch]$AddToPath,
        [switch]$Help
    )

    try {
        if ($Help) {
            Show-Usage
            return
        }

        # Set script-level variables from parameters
        if ($Arch) {
            $script:Arch = $Arch
        }

        if (-not $Prefix) {
            $Prefix = Get-DefaultInstallDir
        }

        $installDir = $Prefix
        $destination = Join-Path $installDir $script:Binary

        $tag = if ($Version -eq "latest") {
            Get-LatestReleaseTag
        }
        else {
            Normalize-Version -Ver $Version
        }

        Write-Log "Installing aibris $tag..."
        $binaryPath = Install-Release -Tag $tag

        Install-Binary -Source $binaryPath -Destination $destination

        if ($AddToPath) {
            Add-ToUserPath -Dir $installDir
        }

        # Verify installation
        try {
            $versionOutput = & $destination --version 2>&1
            Write-Log $versionOutput
        }
        catch {
            Write-Log "Installed but --version check failed: $_"
        }

        if (-not $AddToPath) {
            Show-PathHint -InstallDir $installDir
        }
    }
    finally {
        Invoke-Cleanup
    }
}

# Main execution when script is run directly
if ($MyInvocation.InvocationName -ne '.') {
    if ($Help) {
        Show-Usage
        exit 0
    }

    Install-Aibris -Version $Version -Prefix $Prefix -Arch $Arch -AddToPath:$AddToPath -Help:$Help
}
