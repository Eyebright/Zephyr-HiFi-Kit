# Zephyr HiFi Kit · 版本更新记录 / Changelog

和风HiFi音乐播放套件。最新版本在前，版本号与 Git 标签、GitHub Releases 一致。
Newest first; version numbers match Git tags and GitHub Releases.

## 0.54 — 2026-11-20

**中文**
- 新增：终端通过 ZAT 上报 ALSA 设备名称；服务端播放状态和输出设备下拉显示“名称 · hw”，例如 `AUDIO · hw:3,0`、`b1 · hw:0,0`。
- 兼容：未上报名称的旧 Agent 自动回退为只显示 `hw:X,Y`。
- 说明：这是显示增强，不改变音频路径。Windows 管理版服务端仍只解码 WAV/PCM，FLAC/DSD/AAC 需要 Linux 音频版或转为 WAV。

**English**
- Terminals report ALSA card names over ZAT; server playback status and output selectors show `name · hw`.
- Older agents fall back to hardware ids only. No audio-path change.

## 0.53 — 2026-11-20

**中文**
- 修复：终端自动升级在 `/etc/hifihub/agent.env` 被修改后停在 `dpkg` 配置提示，导致升级中断、运行版本不变。
- 变更：升级安装改为 `dpkg --force-confdef --force-confold` 非交互执行，保留现有配置并避免无 TTY 时卡死。
- 兼容：0.53 起自动升级可正常完成；仍运行 0.4.3 且已受该问题影响的终端，需要手工完成一次到 0.53+ 的安装。

**English**
- Fix: unattended upgrades stalled on the `agent.env` conffile prompt.
- Upgrades now use noninteractive `dpkg --force-confdef --force-confold`, preserving the existing configuration.

## 0.52 — 2026-11-20

**中文**
- 新增：终端中心显示“当前版本 → 服务端目标版本”，支持单台、所选和全部终端的 Agent 直接升级。
- 新增：Agent 上报构建类型（音频版/管理版）；服务端按类型选择安装包（音频包使用 `_audio` 后缀），避免把音频终端刷成管理版。
- 新增：升级进度、失败原因与重试；播放中的终端等待空闲；SHA256 校验与幂等下发。
- 新增：音频分发包导入工具（Linux/Windows），便于服务端同时维护两种构建。
- 修复/说明：0.52 的升级执行器仍可能被 `dpkg` 配置提示阻塞，已由 0.53 修复；0.52 由 0.53 取代。

**English**
- Terminal center shows current vs target Agent version with direct per-terminal and bulk upgrades.
- Agents report audio/management build flavor; the server selects the matching package.
- Superseded by 0.53, which fixes the conffile prompt hang.

## 0.51 — 2026-11-20

**中文**
- 新增：控制台三语界面（简体中文、繁體中文、English），可设置服务端默认语言与浏览器语言偏好。
- 新增：API 错误结构化与本地化（保留旧错误码/原始诊断），日期、数字和相对时间按语言格式化。
- 官网：英文为默认首页，中文版位于 `/zh.html`，导航与页脚可手动切换并保留当前栏目。
- 修复：静态资源改为 `no-cache`，升级后不再沿用旧脚本。

**English**
- Trilingual console (zh-CN, zh-TW, en), server default language and per-browser preference.
- Localized API errors and locale-aware dates/numbers.
- English-first website with a manual Chinese switch.

## 0.50 — 2026-11-19 / 2026-11-20

**中文**
- 品牌统一为 Zephyr HiFi Kit（和风HiFi音乐播放套件），版本号 0.50。
- 产物统一命名：服务端 `Zephyr-HiFi-Kit-server-0.50`，客户端 `Zephyr-HiFi-Kit-terminal-0.50`。
- 新增：Windows amd64 服务端 ZIP、`start.ps1` 启动脚本与 Windows 配置文档。
- 新增：三步使用向导、音乐源复用与增量扫描、扫描失败/取消时保留索引与封面、关于我们合并与免责声明、管理员称呼显示。
- 文档：README.cn.md、README.en.md、INSTALL.md 与官网同步更新。
- 升级：由于更新器文件名变化，0.4.3 → 0.50 需要手动安装一次；数据目录与内部服务标识保持兼容。

**English**
- Unified Zephyr HiFi Kit branding and 0.50 artifact names.
- Added the Windows amd64 server ZIP, `start.ps1`, and Windows setup documentation.
- Added the three-step onboarding, source reuse, incremental scans, safer scan rollback, and consolidated About/disclaimer content.

## 0.4.3 — 2026-11-19

**中文**
- 新增音乐主页、曲库浏览、收藏、集中设置与终端中心。
- 服务端播放队列（上一首/下一首、自动续播）与终端“正在播放”控制台。
- Windows 服务端直连 SMB 共享；APT 安装源与本地包导入；内置 3 个终端席位。
- 修复终端 deb 安装、播放流控与部分控制台交互问题。

**English**
- Music home, library browsing, favorites, centralized settings, and terminal center.
- Server-side playback queue and now-playing console; Windows SMB direct access; APT source and local package imports.

## 0.3.0 — 2026-11-16

**中文**
- 真实音频链路：ZAT 推流、Linux libFLAC 解码与 ALSA 输出，音频版与管理版分离。
- 新增 Android 终端 APK（AAudio 输出），产品更名为 Zephyr Music Player。
- 修复 FLAC 关闭时双重释放、非特权启动与音频包发布流程。

**English**
- Real audio path: ZAT streaming, libFLAC decoding, and ALSA output; audio and management builds separated.
- Android terminal APK with AAudio; early packaging and playback fixes.

## 0.2.0 — 2026-11-13

**中文**
- 首个公开版本：单一下发源的 Hi-Fi 机群管理平台，包含终端注册、签名制品、BOM 期望态、灰度发布与播放门控的基础能力。

**English**
- First public release: single-source Hi-Fi fleet management with terminal enrollment, signed artifacts, BOM desired state, staged rollouts, and playback gating.

## 已知限制 / Known limitations

- Windows 管理版服务端只解码受支持的 WAV/PCM；FLAC、DSD、AAC 等需要 Linux 音频版服务端（libFLAC）或转换为 WAV。
- 管理版 Linux 终端 deb 不含 ALSA 播放；真实播放请使用音频版终端。
- 首次管理员初始化仅允许服务端本机 localhost 或直连 HTTPS。
- 发布与制品可用性以对应 GitHub Release 的自动验证结果为准；下载后请核对 `SHA256SUMS`。
