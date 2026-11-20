# Zephyr HiFi Kit

**和风HiFi音乐播放套件 · Version 0.51 · Release tag `v0.51`**

[Language selection](README.md) · [简体中文](README.cn.md) · [Installation guide](INSTALL.md)

Zephyr HiFi Kit connects music files, a searchable library and network playback terminals. The server reads local directories or NAS shares, provides an embedded web console, manages playback queues and streams audio through ZAT (Zephyr Audio Transport). It also provides terminal enrollment, software delivery, licensing and maintenance.

Servers target Linux amd64/arm64 and Windows amd64. Linux audio terminals use ALSA; Android terminals use AAudio. **The Windows server is neither a Windows desktop audio player nor an automatically registered Windows system service.**

## Capabilities and limits

| Area | Implemented behavior and limitations |
|---|---|
| Music sources | Server-side absolute directories and SMB/NAS shares; Windows uses system SMB/UNC sessions, while Linux has a CIFS backend and can also read pre-mounted directories |
| Source lifecycle | Reuse of an existing physical source; incremental scans skip files whose size and modification time are unchanged; full rescans are available; archiving retains catalog entries and source identity without deleting music files |
| Library | Artist, album, track, genre, year, composer and folder browsing; search, sorting, pagination, favorites, playlists, history and listening statistics |
| Playback | ZAT streaming, per-terminal queues, pause/resume/stop, seeking, volume and output selection; recognizing a format does not guarantee decoding or device compatibility |
| Administrator | Single-administrator password unlock, session cookies and a display name; not a multi-user or role-switching system; sensitive management handlers validate access on the server |
| First run | Skippable welcome screen and three-step setup; persisted display name and progress; skipping does not create a password, install terminals or bypass authorization |
| Terminals | One-time enrollment tokens, online status, per-terminal authorization, DAC and playback-protection settings, Linux software installation and system maintenance |
| Delivery and licensing | Signed artifacts, BOM desired state, jobs, staged delivery and audit logs; 3 built-in terminal seats, expandable with an offline-signed license |
| Maintenance | Music-data export/restore and cleanup; this export is not a complete backup of credentials, node identities or original audio files |

This describes source-code capabilities, not acceptance testing on every platform. The Windows server has run locally, but **the complete 0.51 flow under the new authentication model—initialization, source setup, scanning, enrollment/authorization and actual playback—has not yet been acceptance-tested**. There is no claim that every format, DAC, NAS or synchronized multi-room configuration has been verified.

### Choosing an audio build

- **Management server:** the pure-Go build handles supported integer PCM WAV files. The Linux CGO audio build adds a real libFLAC decoder; FLAC support across the project is not merely a placeholder.
- **Management Linux terminal:** no ALSA playback backend. It supports management and delivery, not audible ZAT playback. Install an audio terminal build to play music.
- **Audio Linux terminal:** CGO + ALSA. Preserving the original sample rate and bit depth depends on the DAC, device selection, volume and other settings; “bit-perfect” output is not an unconditional guarantee.
- **Android:** AAudio requests exclusive mode first and may fall back to shared mode. Results depend on the device and system audio path.
- `ffmpeg`, `ffprobe` and GPG are optional external installations, not bundled with the Windows ZIP. See [INSTALL.md](INSTALL.md) for their function-specific uses and current decoding limits; installing tools does not automatically enable every playback format.

## Get 0.51

Choose `v0.51` in the [existing repository's Releases](https://github.com/Eyebright/Zephyr-HiFi-Kit/releases) and verify the published checksums. The naming table below does not assert that assets have already been uploaded.

| Purpose | Release asset |
|---|---|
| Multi-architecture Linux management server | `Zephyr-HiFi-Kit-server-0.51.tar.gz` |
| Linux audio server | `Zephyr-HiFi-Kit-server-0.51-audio-amd64.tar.gz` / `Zephyr-HiFi-Kit-server-0.51-audio-arm64.tar.gz` |
| Windows amd64 server | `Zephyr-HiFi-Kit-server-0.51-windows-amd64.zip` |
| Linux management terminal debs | `Zephyr-HiFi-Kit-terminal-0.51-linux-amd64.deb` / `Zephyr-HiFi-Kit-terminal-0.51-linux-arm64.deb` / `Zephyr-HiFi-Kit-terminal-0.51-linux-armhf.deb` |
| Linux management terminal collection | `Zephyr-HiFi-Kit-terminal-0.51-linux.tar.gz` |
| Linux audio terminal bundle | `Zephyr-HiFi-Kit-terminal-0.51-audio-amd64.tar.gz` / `Zephyr-HiFi-Kit-terminal-0.51-audio-arm64.tar.gz` |
| Android terminal | `Zephyr-HiFi-Kit-terminal-0.51.apk` |
| Container image | `ghcr.io/eyebright/zephyr-hifi-kit-server:0.51` |

The public product name is Zephyr HiFi Kit. **Internal `hifihub` service names, paths, environment variables and APIs remain compatible.** Terminal debs still declare `Package: hifihub-agent`; the `/dl/` distribution set uses `hifihub-agent_0.51_<arch>.deb`. Do not rename existing configuration just to match the new branding.

## Quick start

### Windows server

Extract the ZIP and enter its same-named directory in PowerShell:

```powershell
Expand-Archive -LiteralPath .\Zephyr-HiFi-Kit-server-0.51-windows-amd64.zip -DestinationPath .
Set-Location .\Zephyr-HiFi-Kit-server-0.51-windows-amd64
.\start.ps1
```

Open `http://localhost:8080/`. The launcher defaults to HTTP `127.0.0.1:8080`, ZAT `0.0.0.0:7800`, and `$env:LOCALAPPDATA\Zephyr-HiFi-Kit\data` for persistent data. Set `HIFIHUB_DATA_DIR` to the original data directory before starting a migrated installation. Server self-update, automatic update application and terminal version convergence default to disabled in this launcher.

If script execution is restricted, do not disable execution policy system-wide. You can run the EXE directly from the extracted directory after setting the environment explicitly:

```powershell
$root = (Get-Location).Path
if (-not $env:HIFIHUB_DATA_DIR) {
    $env:HIFIHUB_DATA_DIR = Join-Path $env:LOCALAPPDATA 'Zephyr-HiFi-Kit\data'
}
$env:HIFIHUB_LISTEN = '127.0.0.1:8080'
$env:HIFIHUB_ZAT_ADDR = '0.0.0.0:7800'
$env:HIFIHUB_DIST_DIR = Join-Path $root 'dist'
$env:HIFIHUB_GPG_HOME = Join-Path $env:HIFIHUB_DATA_DIR 'gnupg'
$env:HIFIHUB_MOUNT_DIR = Join-Path $env:HIFIHUB_DATA_DIR 'mounts'
$env:HIFIHUB_MOUNT_CRED_DIR = Join-Path $env:HIFIHUB_DATA_DIR 'cifs'
$env:HIFIHUB_SIGNATURE_MODE = 'enforce'
$env:HIFIHUB_UPDATE_SELF = 'false'
$env:HIFIHUB_UPDATE_AUTO = 'false'
$env:HIFIHUB_UPDATE_FLEET = 'false'
[System.IO.Directory]::CreateDirectory($env:HIFIHUB_GPG_HOME) | Out-Null
& (Join-Path $root 'Zephyr-HiFi-Kit-server.exe')
```

For LAN access, explicitly set `HIFIHUB_LISTEN` to the desired LAN address and permit only trusted LAN peers through the firewall. TCP 8080 is HTTP/API/downloads; TCP 7800 is ZAT. Changing the bind address does not enable remote administrator login. Read the authentication and reverse-proxy limitations below before exposing a listener.

The Windows distribution set contains management terminal packages. For actual Linux-terminal playback, replace `dist/` with the matching 0.51 audio distribution set: update the installer, audio deb and `SHA256SUMS` together. Do this before enabling fleet convergence; otherwise management packages can replace audio-capable terminals. Keep unrelated architecture files and checksums consistent.

Windows upgrades are manual: stop playback and the server process, back up the complete data directory and configuration, replace the program and distribution files, then restart using the same data path. The package does not promise in-place self-update. Local source paths refer to the server, not the browser's computer; Windows SMB access uses the server account's network session and share permissions.

### Linux server

Install on a trusted network. The Linux template defaults to `0.0.0.0:8080`, so restrict access before startup.

```sh
sudo apt-get update
sudo apt-get install -y gnupg
tar -xzf Zephyr-HiFi-Kit-server-0.51.tar.gz
cd Zephyr-HiFi-Kit-server-0.51/server
sudo ./install.sh
curl -fsS http://127.0.0.1:8080/api/v1/health
```

For native FLAC decoding, use the audio server bundle for the host architecture and install its matching libFLAC runtime. Configuration remains in `/etc/hifihub/server.env`; state remains in `/var/lib/hifihub-server`. A headless server can be administered through an SSH local port forward and a localhost browser URL; see [INSTALL.md](INSTALL.md).

### Welcome and three-step setup

1. **Administrator and security:** create the administrator through localhost on the server. Supply a 1–40-character display name and a password of at least 12 characters. The name personalizes greetings; it is not another login account. Existing administrators sign in instead of creating new credentials.
2. **Add music sources:** under Settings → Music folders, add a local directory or NAS share, enable/mount it, test access and scan. This step may be deferred.
3. **Connect playback terminals:** issue a one-time token in the terminal center, install on the target device manually, confirm it is online, authorize it and select its audio output.

You can skip the welcome screen and resume from Settings → Administrator and preferences. Skipping only hides the guide; it does not mark configuration complete. Finishing the guide returns to the home page without starting playback. Legacy instances with an administrator are not forced to reset their password or repeat onboarding.

**Initial setup is stricter than login:** current code permits initialization only through a local localhost connection. Subsequent login requires localhost or HTTPS recognized by the server. Remote plain HTTP is rejected. The server does not trust `Forwarded`/`X-Forwarded-*`, so merely terminating TLS at a proxy and adding `X-Forwarded-Proto` does not make administrator authentication work. The executable currently serves HTTP; a production HTTPS/proxy integration must be validated rather than assumed. Until then, keep administrative access local or use an SSH loopback tunnel. Do not spoof headers or weaken authorization to work around this restriction.

### Enroll a terminal

Sign in as administrator and open Terminal center → Add terminal to obtain the token and installation command. Use a server LAN address reachable from the terminal, not localhost. The Linux installer chooses the architecture automatically. On Android, install the APK and enter the server URL, token and optional alias. Enrollment, online status and authorization are separate states; the built-in three seats still require explicit per-terminal authorization.

## Upgrades and security

- **Upgrade from 0.4.3 to 0.51 manually once.** The old updater hard-codes the former asset names; the new release cannot repair asset selection in the old running process.
- Windows uses manual process replacement with data preservation. Containers are upgraded by replacing the image while retaining the data volume.
- Linux self-update and terminal convergence depend on enabled settings, suitable assets, verification, runtime permissions and playback gates; unattended upgrades are not guaranteed in every deployment.
- Keep `HIFIHUB_SIGNATURE_MODE=enforce` in production. Install GPG and import trusted public keys when signature verification is needed. Keep signing private keys offline or on a controlled signing host; release packages must not contain private keys, passwords or private music libraries.
- The built-in administrator is not a universal access barrier for all APIs. Restrict network access; do not expose HTTP, ZAT or terminal-local consoles directly to the public Internet.

## Development

`cmd/hifihub-server/` is the server entry point; `internal/` contains APIs, library services, decoders and the embedded console; `agent/` is a separate Go module; `zat/` holds the shared transport; `packaging/` and `scripts/` contain deployment/build tooling. `prototype/` is not evidence of implemented runtime functionality.

Use a Go toolchain satisfying `go.mod` (currently `go 1.27.4`). From the repository root:

```sh
VERSION=0.51 sh scripts/build-release.sh
go test ./...
go -C agent test ./...
node --test internal/web/*.test.cjs
```

Linux audio builds additionally require CGO and libFLAC/ALSA development libraries. Android builds require the SDK/NDK and Gradle. See [INSTALL.md](INSTALL.md) for details. These are reproduction commands, not a claim that they were executed during this documentation update.

[Website](https://www.zephyr.hk) · services@zephyr.hk · MIT [LICENSE](LICENSE)
