# Zephyr HiFi Kit · 和风HiFi音乐播放套件

**版本 / Version: 0.82 · Git tag: `v0.82`**

面向本地音乐库与网络播放终端的音乐播放及管理套件，提供内嵌 Web 控制台、音乐源管理、曲库浏览和 ZAT 音频推送。服务端支持 Linux 与 Windows；实际播放能力取决于解码后端、终端音频构建及设备。

A music playback and management kit for local libraries and network terminals, with an embedded web console, source management, library browsing and ZAT streaming. Servers run on Linux and Windows; playback capabilities depend on the decoder, terminal build and audio device.

0.82 highlights: scans commit in batches and resume after a restart while streaming stays responsive, cached lyrics with hard timeouts, an Android AAudio negotiation ladder that fixes error -898 and native-clock high-rate playback, a new `hifihub-player` MCP agent plugin shipped per platform, and one version line where three-part tags publish Android-only hot fixes. 0.82 更新：扫描分批提交且重启续播、播放不再被扫描阻塞，歌词缓存与硬超时，安卓 AAudio 协商梯修复 -898 并按 DAC 原生时钟播高码率，新增随发布分发各平台安装包的 `hifihub-player` 智能体插件，版本号统一且三段号只发安卓热修 APK。

## 选择语言 / Choose a language

- [简体中文 README](README.cn.md)
- [English README](README.en.md)
- [安装与部署 / Installation guide](INSTALL.md) — 含 Windows 服务端配置 / includes Windows server setup
- [版本更新记录 / Changelog](CHANGELOG.md)

[项目仓库 / Repository](https://github.com/Eyebright/Zephyr-HiFi-Kit) · [官网 / Website](https://www.zephyr.hk) · services@zephyr.hk

MIT · [LICENSE](LICENSE)
