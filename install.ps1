<#
.SYNOPSIS
Install codexctl on Windows.

.DESCRIPTION
Downloads the codexctl release archive for this machine from GitHub, checks
its SHA-256 against the release's checksums.txt, and places codexctl.exe in
the install directory. The directory is added to the user's PATH when it is
not already there.

    irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1 | iex

To pass options, download the script first or use a script block:

    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1))) -Version 0.2.0

CODEXCTL_VERSION and CODEXCTL_INSTALL_DIR set the same options.

The Ed25519 signature on checksums.txt is not checked here because Windows
PowerShell has no Ed25519 support. Run "codexctl update" afterwards to have
the installed binary re-verify itself against the signed release.

.PARAMETER Version
Install this release instead of the latest one, for example 0.2.0.

.PARAMETER InstallDir
Install into this directory. Defaults to $env:LOCALAPPDATA\Programs\codexctl.

.PARAMETER NoModifyPath
Do not add the install directory to the user's PATH.
#>
[CmdletBinding()]
param(
    [string]$Version = $env:CODEXCTL_VERSION,
    [string]$InstallDir = $env:CODEXCTL_INSTALL_DIR,
    [switch]$NoModifyPath
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'AbdelrhmanSaid/codexctl'
$ReleasesUrl = "https://github.com/$Repo/releases"

# Windows PowerShell 5.1 defaults to TLS 1.0 on older systems; GitHub needs 1.2.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch { }

function Get-Arch {
    $arch = $null
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    switch -Regex ($arch) {
        '^(X64|AMD64)$' { return 'amd64' }
        '^ARM64$'       { return 'arm64' }
        default { throw "unsupported CPU architecture: $arch (releases exist for x86-64 and ARM64)" }
    }
}

function Get-Sha256([string]$Path) {
    (Get-FileHash -Path $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

$arch = Get-Arch
$Version = $Version -replace '^v', ''
$base = if ($Version) { "$ReleasesUrl/download/v$Version" } else { "$ReleasesUrl/latest/download" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("codexctl-install-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host 'Fetching release manifest...'
    $checksumsPath = Join-Path $tmp 'checksums.txt'
    try {
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksumsPath -UseBasicParsing
    } catch {
        throw "cannot download $base/checksums.txt; check the version and your network ($($_.Exception.Message))"
    }

    $checksums = @{}
    foreach ($line in Get-Content $checksumsPath) {
        $fields = $line -split '\s+' | Where-Object { $_ }
        if ($fields.Count -eq 2 -and $fields[0] -match '^[0-9a-fA-F]{64}$') {
            $checksums[$fields[1]] = $fields[0].ToLowerInvariant()
        }
    }
    if (-not $Version) {
        $first = $checksums.Keys | Where-Object { $_ -like 'codexctl_*' } | Select-Object -First 1
        if (-not $first) { throw 'checksums.txt lists no codexctl assets' }
        $Version = ($first -split '_')[1]
    }

    $asset = "codexctl_${Version}_windows_${arch}.zip"
    if (-not $checksums.ContainsKey($asset)) {
        throw "release $Version has no asset for windows/$arch"
    }

    Write-Host "Downloading codexctl $Version for windows/$arch..."
    $assetPath = Join-Path $tmp $asset
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $assetPath -UseBasicParsing

    $actual = Get-Sha256 $assetPath
    if ($actual -ne $checksums[$asset]) {
        throw "SHA-256 mismatch for ${asset}: expected $($checksums[$asset]), got $actual"
    }
    Write-Host 'Checksum verified.'

    $extract = Join-Path $tmp 'extract'
    Expand-Archive -Path $assetPath -DestinationPath $extract -Force
    $binary = Get-ChildItem -Path $extract -Recurse -Filter 'codexctl.exe' | Select-Object -First 1
    if (-not $binary) { throw "archive $asset does not contain codexctl.exe" }

    if (-not $InstallDir) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\codexctl'
    }
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir 'codexctl.exe'

    # A running codexctl.exe cannot be overwritten but can be renamed, which
    # is also how "codexctl update" replaces itself.
    if (Test-Path $target) {
        $old = "$target.old"
        Remove-Item $old -Force -ErrorAction SilentlyContinue
        Move-Item -Path $target -Destination $old -Force
        try {
            Copy-Item -Path $binary.FullName -Destination $target -Force
        } catch {
            Move-Item -Path $old -Destination $target -Force
            throw
        }
        Remove-Item $old -Force -ErrorAction SilentlyContinue
    } else {
        Copy-Item -Path $binary.FullName -Destination $target -Force
    }

    Write-Host "Installed codexctl $Version to $target"

    $onPath = ($env:Path -split ';' | Where-Object { $_ } | ForEach-Object { $_.TrimEnd('\') }) -contains $InstallDir.TrimEnd('\')
    if (-not $onPath) {
        if ($NoModifyPath) {
            Write-Host ''
            Write-Host "$InstallDir is not on your PATH. Add it to use codexctl from any terminal."
        } else {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            $userEntries = @($userPath -split ';' | Where-Object { $_ } | ForEach-Object { $_.TrimEnd('\') })
            if ($userEntries -notcontains $InstallDir.TrimEnd('\')) {
                $newUserPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
                [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
            }
            $env:Path = "$env:Path;$InstallDir"
            Write-Host "Added $InstallDir to your user PATH. Open a new terminal for it to take effect."
        }
    }

    $other = Get-Command codexctl.exe -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($other -and $other.Source -ne $target) {
        Write-Warning "another codexctl at $($other.Source) comes first on PATH; remove it or reorder PATH"
    }

    Write-Host "Run 'codexctl update' later to upgrade in place."
} finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
