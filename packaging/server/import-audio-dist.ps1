# Imports one audio terminal bundle into a Windows server's dist directory so
# audio-build terminals upgrade with the ALSA-capable deb. Management filenames
# stay unchanged; the audio copy gets a _audio suffix.
#   .\import-audio-dist.ps1 -DistDir C:\path\dist -Bundle C:\path\Zephyr-HiFi-Kit-terminal-0.54-audio-arm64.tar.gz
param(
    [Parameter(Mandatory = $true)][string]$DistDir,
    [Parameter(Mandatory = $true)][string]$Bundle
)
$ErrorActionPreference = 'Stop'
$DistDir = (Resolve-Path -LiteralPath $DistDir).Path
$Bundle = (Resolve-Path -LiteralPath $Bundle).Path
if (-not (Test-Path -LiteralPath $DistDir -PathType Container)) { throw "Dist directory not found: $DistDir" }

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("zephyr-audio-dist-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
try {
    tar -xzf $Bundle -C $temp
    $deb = Get-ChildItem -LiteralPath $temp -Recurse -Filter 'hifihub-agent_*_*.deb' -File | Select-Object -First 1
    if (-not $deb) { throw 'Bundle has no terminal deb' }
    $agentDir = $deb.DirectoryName
    $sumFile = Join-Path $agentDir 'SHA256SUMS'
    if (-not (Test-Path -LiteralPath $sumFile -PathType Leaf)) { throw 'Bundle has no agent SHA256SUMS' }
    foreach ($line in Get-Content -LiteralPath $sumFile) {
        if ($line -notmatch '^([a-fA-F0-9]{64}) [ *](.+)$') { throw "Invalid checksum line: $line" }
        $file = Join-Path $agentDir $Matches[2]
        $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $Matches[1].ToLower()) { throw "Checksum mismatch: $file" }
    }

    $name = $deb.BaseName + '_audio.deb'
    Copy-Item -LiteralPath $deb.FullName -Destination (Join-Path $DistDir $name) -Force

    $lines = Get-ChildItem -LiteralPath $DistDir -File |
        Where-Object { $_.Name -like 'hifihub-agent_*.deb' -or $_.Name -eq 'agent-install.sh' } |
        Sort-Object Name |
        ForEach-Object { "$((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower())  $($_.Name)" }
    Set-Content -LiteralPath (Join-Path $DistDir 'SHA256SUMS') -Value $lines -Encoding ascii

    "Imported $name"
    'Dist SHA256SUMS refreshed'
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
