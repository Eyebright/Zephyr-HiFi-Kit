# Zephyr HiFi Kit Windows management server. Run from any working directory.
# Set HIFIHUB_* environment variables before invoking this script to override
# defaults. No password, ACL, firewall or execution-policy changes are made.
$ErrorActionPreference = 'Stop'

function Set-DefaultEnvironment([string] $Name, [string] $Value) {
    if ([string]::IsNullOrEmpty([Environment]::GetEnvironmentVariable($Name, 'Process'))) {
        [Environment]::SetEnvironmentVariable($Name, $Value, 'Process')
    }
}

$server = Join-Path $PSScriptRoot 'Zephyr-HiFi-Kit-server.exe'
if (-not (Test-Path -LiteralPath $server -PathType Leaf)) {
    throw "Server executable not found: $server"
}
if ([string]::IsNullOrEmpty($env:HIFIHUB_DATA_DIR)) {
    $localData = $env:LOCALAPPDATA
    if ([string]::IsNullOrEmpty($localData)) {
        $localData = [Environment]::GetFolderPath('LocalApplicationData')
    }
    if ([string]::IsNullOrEmpty($localData)) { throw 'LOCALAPPDATA is unavailable; set HIFIHUB_DATA_DIR.' }
    $env:HIFIHUB_DATA_DIR = Join-Path $localData 'Zephyr-HiFi-Kit/data'
}
Set-DefaultEnvironment 'HIFIHUB_LISTEN' '127.0.0.1:8080'
Set-DefaultEnvironment 'HIFIHUB_ZAT_ADDR' '0.0.0.0:7800'
Set-DefaultEnvironment 'HIFIHUB_DIST_DIR' (Join-Path $PSScriptRoot 'dist')
Set-DefaultEnvironment 'HIFIHUB_GPG_HOME' (Join-Path $env:HIFIHUB_DATA_DIR 'gnupg')
Set-DefaultEnvironment 'HIFIHUB_MOUNT_DIR' (Join-Path $env:HIFIHUB_DATA_DIR 'mounts')
Set-DefaultEnvironment 'HIFIHUB_MOUNT_CRED_DIR' (Join-Path $env:HIFIHUB_DATA_DIR 'cifs')
Set-DefaultEnvironment 'HIFIHUB_SIGNATURE_MODE' 'enforce'
Set-DefaultEnvironment 'HIFIHUB_PLAY_GATE' 'true'
Set-DefaultEnvironment 'HIFIHUB_UPDATE_SELF' 'false'
Set-DefaultEnvironment 'HIFIHUB_UPDATE_AUTO' 'false'
# Management dist is not audio-enabled. Only consider fleet convergence after
# deliberately replacing dist with matching audio packages.
Set-DefaultEnvironment 'HIFIHUB_UPDATE_FLEET' 'false'

# Preserve existing directories and their contents. Only public verification
# keys belong in gnupg; release signing remains private and outside this bundle.
foreach ($directory in @($env:HIFIHUB_DATA_DIR, $env:HIFIHUB_GPG_HOME,
                         $env:HIFIHUB_MOUNT_DIR, $env:HIFIHUB_MOUNT_CRED_DIR)) {
    if (Test-Path -LiteralPath $directory) {
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            throw "Expected a directory: $directory"
        }
    } else {
        [System.IO.Directory]::CreateDirectory($directory) | Out-Null
    }
}
if (-not (Test-Path -LiteralPath $env:HIFIHUB_DIST_DIR -PathType Container)) {
    throw "Distribution directory not found: $env:HIFIHUB_DIST_DIR"
}

# Direct invocation preserves the caller's environment and exit status. The
# executable and default dist never depend on the caller's working directory.
& $server
exit $LASTEXITCODE
