# Zephyr HiFi Kit · 和风HiFi音乐播放套件

**版本 0.68 · 发布标签 `v0.68`**

[语言入口](README.md) · [English](README.en.md) · [安装与部署](INSTALL.md) · [版本更新](CHANGELOG.md)

Zephyr HiFi Kit 把音乐文件、曲库和播放终端连接起来：服务端读取本地目录或 NAS 共享，通过内嵌 Web 控制台浏览音乐、管理播放队列，再经 ZAT（Zephyr Audio Transport）向终端推送音频。同时保留终端注册、软件下发、授权与维护功能。

服务端支持 Linux amd64/arm64 和 Windows amd64；Linux 播放终端使用 ALSA 音频版，Android 终端使用 AAudio。**Windows 服务端不是 Windows 本地播放器，也不是自动注册的 Windows 系统服务。**

## 0.68 更新要点

- 每设备独立 DSP：16 段图形 EQ、常用参考曲线、最多 8 组 PEQ、前置增益与软限幅；设置按 `node_id::hw` 记忆，MQA/DoP 位完美路径自动绕过 EQ。
- 安全 DSD 与设备状态：DSD 默认转 PCM，按设备显式选择 DoP，避免不支持 DoP 的 DAC 把标记帧当 PCM 播放产生噪音；队列、顺序、音量和定时关闭也按播放设备隔离。
- 音频设备中心：设置页内集中管理播放设备，支持激活、命名、DSD 模式、EQ 状态与删除终端；播放栏和控制台只显示播放设备名。
- 管理与接入：本机 localhost 自动获得管理员会话，不再反复输入密码；远程管理仍需登录。安卓安装令牌改为 6–8 位数字验证码。
- 曲库与音频修复：补强 ID3/DSF/DFF/AIFF/Ogg/MP4 标签读取、目录兜底及可预览的安全回填；修复高采样率/高位深回环投送噪音与无年份专辑的推送按钮。

## 当前能力与边界

| 领域 | 已实现能力与限制 |
|---|---|
| 音乐源 | 服务端本地绝对路径、SMB/NAS；Windows 通过系统 SMB/UNC 会话连接，Linux 使用 CIFS 后端，也可添加预挂载目录 |
| 源生命周期 | 同一实际来源复用已有源；默认增量扫描，按文件大小与修改时间跳过未变化文件；可完全重新扫描；归档保留索引与来源身份，不删除原始音乐 |
| 曲库 | 艺术家、专辑、曲目、流派、年份、作曲家、文件夹浏览；搜索、排序、分页、收藏、播放列表、历史与聆听统计 |
| 播放 | ZAT 推流、每播放设备独立队列与模式（空闲设备入队即播）、暂停/恢复/停止、定位、音量、DSD PCM/DoP、16 段 EQ/PEQ 与定时关闭；格式识别不等于可解码，也不等于设备一定支持输出 |
| 管理员 | 单管理员、会话 Cookie 与姓名/称呼；本机 localhost 自动获得可信会话，远程管理仍需显式登录；不是多用户账号系统或角色切换，敏感接口在服务端再次校验 |
| 首次使用 | 可跳过的欢迎页与三步引导；称呼和进度持久化；跳过不会创建密码、自动安装终端或绕过权限 |
| 终端管理 | 一次性注册令牌、SSH 自动安装（主机指纹确认）、在线状态、逐终端授权、DAC 与播放保护配置、Linux 软件安装及系统维护 |
| 软件与授权 | 签名制品、BOM 期望态、任务与灰度下发、审计；应用目录区分公开应用与自备上传；服务端内置 3 个终端席位，可导入离线签名许可证扩容 |
| 备份与维护 | 音乐数据导出/恢复、清理；音乐数据导出不是包含密码、节点身份和原音频的整机备份 |

这些是源码能力说明，不是全部平台的验收结论。0.68 由 CI 自动验证（含 Windows 实机任务与音频构建测试），本机生产环境亦持续验证；仍不承诺全部音频格式、任意 DAC、所有 NAS 或多房间同步均已验证。

### 音频版本怎么选

- **管理版服务端**：纯 Go 构建可解码 FLAC（16/24/32-bit）与受支持的整数 PCM WAV；Linux CGO 音频版继续使用 libFLAC。MQA / MQA-CD 可识别并位完美直通至 DAC（不重采样、锁定软件音量），展开由 DAC 完成；DSD 默认转换为 PCM，按播放设备显式选择 DoP 后才位完美直通；不支持 DoP 时使用 ffmpeg PCM 回退；原生 DSD 传输待后续版本。
- **管理版 Linux 终端**：没有 ALSA 播放后端，用于管理与下发，不具备 ZAT 实际出声能力。真实播放须换装音频版终端。
- **音频版 Linux 终端**：CGO + ALSA；输出能否保持原采样率/位深取决于 DAC、设备选择和音量等设置，不作无条件“比特完美”保证。
- **Android**：AAudio 优先请求独占模式，失败时可回退共享模式；实际效果取决于设备和系统音频路径。
- `ffmpeg`、`ffprobe`、GPG 均不随 Windows 包捆绑。其按功能安装的用途与当前解码限制见 [INSTALL.md](INSTALL.md)；不要假定安装外部工具便自动扩展所有播放能力。

## 获取 0.68

从[现有项目 Releases](https://github.com/Eyebright/Zephyr-HiFi-Kit/releases)选择 `v0.68`。包名与用途如下，下载后核对发布校验和；此表不表示资产已经上传。

| 用途 | 发布资产 |
|---|---|
| Linux 多架构服务端管理包 | `Zephyr-HiFi-Kit-server-0.68.tar.gz` |
| Linux 服务端音频包 | `Zephyr-HiFi-Kit-server-0.68-audio-amd64.tar.gz` / `Zephyr-HiFi-Kit-server-0.68-audio-arm64.tar.gz` |
| Windows amd64 服务端 | `Zephyr-HiFi-Kit-server-0.68-windows-amd64.zip` |
| Linux 终端管理 deb | `Zephyr-HiFi-Kit-terminal-0.68-linux-amd64.deb` / `Zephyr-HiFi-Kit-terminal-0.68-linux-arm64.deb` / `Zephyr-HiFi-Kit-terminal-0.68-linux-armhf.deb` |
| Linux 终端管理合集 | `Zephyr-HiFi-Kit-terminal-0.68-linux.tar.gz` |
| Linux 音频终端包 | `Zephyr-HiFi-Kit-terminal-0.68-audio-amd64.tar.gz` / `Zephyr-HiFi-Kit-terminal-0.68-audio-arm64.tar.gz` |
| Android 终端 | `Zephyr-HiFi-Kit-terminal-0.68.apk` |
| 容器镜像 | `ghcr.io/eyebright/zephyr-hifi-kit-server:0.68` |

对外名称统一为 Zephyr HiFi Kit，**内部 `hifihub` 的服务名、目录、环境变量和 API 保持兼容**。终端 deb 的内部 `Package` 仍为 `hifihub-agent`，`/dl/` 分发集仍使用 `hifihub-agent_0.68_<arch>.deb`，不要仅为改名迁移现有配置。

## 快速开始

### Windows 服务端

解压 ZIP，在 PowerShell 中进入解压后的同名目录：

```powershell
Expand-Archive -LiteralPath .\Zephyr-HiFi-Kit-server-0.68-windows-amd64.zip -DestinationPath .
Set-Location .\Zephyr-HiFi-Kit-server-0.68-windows-amd64
.\start.ps1
```

打开 `http://localhost:8080/`。脚本默认 HTTP `127.0.0.1:8080`、ZAT `0.0.0.0:7800`，数据放在 `$env:LOCALAPPDATA\Zephyr-HiFi-Kit\data`。迁移时在启动前用 `HIFIHUB_DATA_DIR` 指回原数据目录。脚本默认关闭服务端自更新、自动安装更新与终端版本收敛。

若 PowerShell 执行策略限制脚本，不要全局关闭执行策略；[安装指南](INSTALL.md)提供手动设置环境变量并直接运行 EXE 的等价方法。LAN 访问须明确配置 `HIFIHUB_LISTEN`、仅向可信 LAN 开放防火墙，并考虑 TLS/反代限制。

### Linux 服务端

在可信网络环境安装；默认 Linux 模板监听 `0.0.0.0:8080`，应先限制网络入口。

```sh
sudo apt-get update
sudo apt-get install -y gnupg
tar -xzf Zephyr-HiFi-Kit-server-0.68.tar.gz
cd Zephyr-HiFi-Kit-server-0.68/server
sudo ./install.sh
curl -fsS http://127.0.0.1:8080/api/v1/health
```

需要服务端原生 FLAC 解码时改用匹配架构的音频包，并安装匹配的 libFLAC 运行库。无桌面的 Linux 机器可使用 SSH 本地端口转发，在浏览器通过 localhost 完成首次设置；详见安装指南。

### 欢迎页与三步引导

1. **管理员与安全**：在服务端本机 localhost 创建管理员，填写 1–40 字符的姓名/称呼及至少 12 字符的密码。称呼用于首页问候，不是新登录账号；已有管理员在本机 localhost 使用时自动登录，远程使用仍需输入密码。
2. **添加音乐源**：在“设置 → 音乐文件夹”添加本地目录或 NAS 共享，启用/挂载、检测连接并扫描；也可稍后配置。
3. **连接播放终端**：在音频设备签发一次性令牌，在目标设备上手动安装，确认上线并授权，然后选择音频输出。

欢迎页可选“跳过，我自己配置”，之后从“设置 → 管理员与偏好”继续。跳过只是隐藏引导，不代表完成配置；“开始聆听”结束引导并返回主页，不会自动播放。已有管理员的旧实例不会被强制重设密码或重走引导。

**初始化比登录更严格**：当前实现只允许本机 localhost 初始化；已有管理员在本机 localhost 会自动获得会话。远程登录要求服务端能够识别的 HTTPS 安全上下文，普通远程 HTTP 不允许登录；仅添加 `X-Forwarded-Proto` 的 TLS 反代也不会获得信任。实际部署边界见 [INSTALL.md](INSTALL.md)。

### 连接终端

先以管理员身份打开“音频设备 → 添加终端”，获取一次性注册令牌与安装命令。使用终端能访问的服务端 LAN 地址，不能照搬 localhost。Linux 安装脚本自动选择架构；Android 安装 APK 后填写地址、6–8 位数字验证码和可选别名。注册、上线、授权是三个不同状态，内置 3 席也需要逐台授权。

音频设备会显示每台终端的当前版本、服务端目标版本和构建类型（音频版/管理版），可单台或批量直接升级 Agent。升级会校验 SHA256，播放中的终端等待空闲，服务端按构建类型选择安装包，不会把音频终端换成管理版；音频包导入方法见 INSTALL.md 6.3。

Windows 包内 `dist/` 是管理版终端分发集。要实际出声，按 [INSTALL.md](INSTALL.md)替换为同版本、匹配架构的音频分发集（脚本、deb、`SHA256SUMS` 一起更新），再安装音频终端；不应只打开自动收敛开关。

## 升级与安全

- **从 0.4.3 升到 0.68 必须手动安装一次**：旧自动更新器硬编码旧包名，不能靠新版本修复旧进程的资产选择。
- Windows 使用“停止进程 → 完整备份数据和配置 → 替换程序/分发集 → 保留原数据目录启动”的手动升级方式，不承诺自更新。容器通过更换镜像并保留数据卷升级。
- Linux 自升级与终端版本收敛受启用状态、发布资产、校验、运行权限和播放保护约束，不保证所有安装环境无操作自动升级。
- 生产保持 `HIFIHUB_SIGNATURE_MODE=enforce`；需要签名校验时安装 GPG 并导入可信公钥。私钥留在离线/受控签名端；发布包不包含私钥、密码或私人曲库。
- 内置管理员不是所有 API 的统一网络访问屏障；仍须限制网络入口，不能把 HTTP、ZAT 或终端本地控制台直接暴露公网。

## 开发与源码

`cmd/hifihub-server/` 为服务端入口；`internal/` 包含 API、曲库、解码、管理与内嵌控制台；`agent/` 为终端独立 Go 模块；`zat/` 为共享传输协议；`packaging/` 与 `scripts/` 提供部署和构建脚本。`prototype/` 是原型，不应作为已实现功能的依据。

使用满足 `go.mod` 的 Go 工具链（当前声明 `go 1.27.4`）。在仓库根目录运行：

```sh
VERSION=0.68 sh scripts/build-release.sh
go test ./...
go -C agent test ./...
node --test internal/web/*.test.cjs
```

Linux 音频构建另需 CGO、libFLAC、ALSA 开发库；Android 需 SDK/NDK 与 Gradle，详见 [INSTALL.md](INSTALL.md)。以上为复现命令，不代表本次文档更新已运行这些测试。

[官网](https://www.zephyr.hk) · services@zephyr.hk · MIT [LICENSE](LICENSE)
