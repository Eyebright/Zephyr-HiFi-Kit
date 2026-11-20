# Zephyr HiFi Kit · 和风HiFi音乐播放套件 — 安装与部署

**版本 0.58 · Git tag `v0.58`**

[语言入口](README.md) · [中文 README](README.cn.md) · [English README / Windows quick start](README.en.md)

本指南区分服务端管理能力、解码能力与终端实际出声能力。Windows 服务端已有本机运行记录；**新认证下从初始化到真实播放的完整黄金路径尚未实测验收**，不能据此宣称全平台、全格式或所有硬件已通过验收。

## 1. 选择发布包

下载入口：[项目 Releases](https://github.com/Eyebright/Zephyr-HiFi-Kit/releases)。选择 `v0.58`，核对文件名、架构及发布校验和；以下为 0.58 交付命名，不代表资产已上传。

| 场景 | 文件名 / 镜像 |
|---|---|
| Linux amd64 + arm64 服务端管理包 | `Zephyr-HiFi-Kit-server-0.58.tar.gz` |
| Linux amd64 服务端音频包 | `Zephyr-HiFi-Kit-server-0.58-audio-amd64.tar.gz` |
| Linux arm64 服务端音频包 | `Zephyr-HiFi-Kit-server-0.58-audio-arm64.tar.gz` |
| Windows amd64 服务端 | `Zephyr-HiFi-Kit-server-0.58-windows-amd64.zip` |
| Linux amd64 终端管理 deb | `Zephyr-HiFi-Kit-terminal-0.58-linux-amd64.deb` |
| Linux arm64 终端管理 deb | `Zephyr-HiFi-Kit-terminal-0.58-linux-arm64.deb` |
| Linux armhf 终端管理 deb | `Zephyr-HiFi-Kit-terminal-0.58-linux-armhf.deb` |
| Linux 多架构终端管理合集 | `Zephyr-HiFi-Kit-terminal-0.58-linux.tar.gz` |
| Linux amd64 音频终端包 | `Zephyr-HiFi-Kit-terminal-0.58-audio-amd64.tar.gz` |
| Linux arm64 音频终端包 | `Zephyr-HiFi-Kit-terminal-0.58-audio-arm64.tar.gz` |
| Android 终端 | `Zephyr-HiFi-Kit-terminal-0.58.apk` |
| Linux 容器镜像 | `ghcr.io/eyebright/zephyr-hifi-kit-server:0.58` |

Linux 服务端管理包解压布局：

```text
Zephyr-HiFi-Kit-server-0.58/
├── server/
│   ├── hifihub-server-linux-amd64
│   ├── hifihub-server-linux-arm64
│   ├── install.sh
│   ├── uninstall.sh
│   ├── server.env
│   ├── hifihub-server.service
│   └── dist/
│       ├── agent-install.sh
│       ├── hifihub-agent_0.58_amd64.deb
│       ├── hifihub-agent_0.58_arm64.deb
│       ├── hifihub-agent_0.58_armhf.deb
│       └── SHA256SUMS
├── agent/
└── SHA256SUMS
```

`agent/` 是终端分发副本。Linux 服务端音频包按架构提供，使用相应音频构建与分发集。独立音频终端包中包含 `agent-install.sh`、`hifihub-agent_0.58_amd64.deb` 或 `hifihub-agent_0.58_arm64.deb`，以及配套 `SHA256SUMS`。

**品牌名不是内部协议迁移。** Linux 安装后的二进制、systemd 服务、`/etc/hifihub`、环境变量 `HIFIHUB_*`、`/api/v1/` 和 `/dl/` 保持原名。对外终端 deb 更名，但内部 `Package: hifihub-agent`、安装服务 `hifihub-agent` 不变；不要把现有服务和目录手工改名。

## 2. Windows 服务端

### 2.1 解压和启动

适用于 Windows amd64。无需安装 Linux、systemd 或 ALSA；这是前台服务端进程，启动脚本不负责注册 Windows Service。先确认 HTTP 8080、ZAT TCP 7800 未被其他程序占用。

解压目录根应直接包含 `Zephyr-HiFi-Kit-server.exe`、`start.ps1`、`dist/` 和随包 README，而不是再套一层 `server/`：

```text
Zephyr-HiFi-Kit-server-0.58-windows-amd64/
├── Zephyr-HiFi-Kit-server.exe
├── start.ps1
├── dist/
└── README（随包说明）
```

在下载目录打开 PowerShell：

```powershell
Expand-Archive -LiteralPath .\Zephyr-HiFi-Kit-server-0.58-windows-amd64.zip -DestinationPath .
Set-Location .\Zephyr-HiFi-Kit-server-0.58-windows-amd64
.\start.ps1
```

保持窗口运行，在浏览器打开 `http://localhost:8080/`。可在另一个 PowerShell 窗口检查：

```powershell
Invoke-RestMethod http://localhost:8080/api/v1/health
```

核对返回的 `status`、`version` 和 `platform`，应对应当前程序；HTTP 健康检查成功不代表管理员认证、扫描或播放已经验证。

### 2.2 启动脚本默认配置

| 环境变量 | Windows 启动默认值 | 用途 |
|---|---|---|
| `HIFIHUB_LISTEN` | `127.0.0.1:8080` | HTTP 控制台、API 与终端包下载，仅本机 |
| `HIFIHUB_ZAT_ADDR` | `0.0.0.0:7800` | ZAT TCP 监听；仍需用防火墙限制来源 |
| `HIFIHUB_DATA_DIR` | `$env:LOCALAPPDATA\Zephyr-HiFi-Kit\data` | 持久状态目录 |
| `HIFIHUB_DIST_DIR` | `$PSScriptRoot\dist` | 随包终端分发集，由 `/dl/` 提供 |
| `HIFIHUB_GPG_HOME` | 数据目录下 `gnupg` | GPG 公钥信任根 |
| `HIFIHUB_MOUNT_DIR` | 数据目录下 `mounts` | 挂载工作目录；Windows SMB 实际走 UNC |
| `HIFIHUB_MOUNT_CRED_DIR` | 数据目录下 `cifs` | 凭据工作目录设置；Windows 连接用 WNet API |
| `HIFIHUB_UPDATE_SELF` | `false` | 不启用服务端自更新 |
| `HIFIHUB_UPDATE_AUTO` | `false` | 不自动安装服务端更新 |
| `HIFIHUB_UPDATE_FLEET` | `false` | 不自动向终端收敛版本；按需显式启用 |

二进制直接运行的通用默认值不等于上述脚本默认值。例如通用 HTTP 默认是 `0.0.0.0:8080`，分发目录默认是 Linux 路径，因此不要不带配置直接双击 EXE。

数据库默认位于数据目录的 `hifihub.db`，管理员凭据文件为 `settings-admin.json`。迁移时保留整个数据目录，并在启动前指向原目录：

```powershell
# 改成已有实例实际使用的目录，不要另建空目录代替
$env:HIFIHUB_DATA_DIR = 'D:\ZephyrData'
.\start.ps1
```

如原部署还设置了 `HIFIHUB_DB_PATH`、`HIFIHUB_ARTIFACTS_DIR` 等独立路径，也要保留相应配置与数据。环境变量在当前 PowerShell 会话设置；新的会话需重新设置。不要把密码写进启动脚本。

### 2.3 PowerShell 限制脚本执行时

不要建议或执行系统范围的执行策略关闭。可以在解压目录手动设置环境变量后直接启动 EXE；这是等价的进程启动方式，不修改执行策略：

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

既有配置若有其他自定义路径，请一并核对；不要以管理员权限或关闭系统保护代替修复目录权限和启动配置。

### 2.4 LAN 与防火墙

默认 HTTP 只允许本机。需要终端或其他可信 LAN 设备访问时，在启动前明确指定绑定地址：

```powershell
# 允许所有本机网卡监听；更收敛的做法是绑定本机实际 LAN IPv4 地址
$env:HIFIHUB_LISTEN = '0.0.0.0:8080'
.\start.ps1
```

- Windows 防火墙只向可信 LAN 地址/子网放行 TCP 8080、7800，优先仅限专用网络配置文件；不要开放到公网或关闭防火墙。
- 终端填写服务端真实 LAN 地址，不能填终端自己的 `localhost`。改变 `HIFIHUB_LISTEN` **不会**使远程 HTTP 可以登录管理员。
- HTTP 控制台/API、终端安装下载和 ZAT 是不同用途；HTTP 的 TLS 反代不自动保护 ZAT 7800。
- 远程管理应采用经过验证的 TLS/反代部署；当前认证对代理有严格限制，见第 3 节。未完成该集成前保留本机管理，不要伪造请求头绕过认证。

### 2.5 外部依赖与 Windows 音频边界

外部工具按功能单独安装到可信位置，需供服务端启动账户访问；若需 PATH，在启动服务端的 PowerShell 会话中配置，安装后重启进程。Windows ZIP 不打包这些工具、私钥、密码或私人音乐库。

| 工具 / 功能 | 是否需要 |
|---|---|
| GPG（可执行文件 `gpg`） | 单纯启动控制台不要求；`enforce` 制品验签、公钥管理、签名许可证验证时需要。未安装时相关操作失败，不应改用 `dev` 绕过 |
| `ffprobe` | 可选的人工媒体参数诊断工具；当前扫描使用项目内格式探测代码，不能把系统安装了 ffprobe 当作扫描已覆盖所有格式 |
| `ffmpeg` | 可选的外部格式转换工具；当前解码源码没有自动调用它的通用回退。可自行生成兼容 WAV 副本再扫描，不要覆盖原文件 |
| Linux `libFLAC` / ALSA | 不适用于 Windows 终端输出；服务端原生 FLAC 解码是 Linux CGO 构建能力，ALSA 是 Linux 音频终端能力 |

当前纯 Go/Windows 解码路径支持 FLAC（16/24/32-bit）与受支持的整数 PCM WAV；Linux CGO 音频版继续使用 libFLAC。MQA 只能解出 MQA core PCM，完整展开依赖 MQA DAC；DSD 透传尚未提供。音频格式、MQA/DSD 能力与设备是否可播放应以所用构建和终端能力为准。

Windows 的 `dist/` 默认放管理版 Linux 终端，**没有 ALSA 实际播放后端**。需要真实播放时：

1. 下载同版本、目标架构的音频终端包。
2. 校验后把其 `agent-install.sh`、`hifihub-agent_0.58_<arch>.deb`、`SHA256SUMS` 作为一个完整分发集放入 `HIFIHUB_DIST_DIR` 指向的目录；脚本应烘焙版本 `0.58`。
3. 多架构分发时核对各架构均是预期音频/管理类型，合并目录后重新生成与实际内容匹配的校验清单；不要用单架构清单冒充完整合集。
4. 在终端安装音频包并验证输出。仅打开 `HIFIHUB_UPDATE_FLEET` 不会把管理构建变成音频构建；错误分发集还可能覆盖已有音频终端。

## 3. 管理员、欢迎页与安全上下文

### 3.1 首次管理员设置

在服务端本机打开 `http://localhost:8080/`（或 `http://127.0.0.1:8080/`），从欢迎引导或“设置 → 管理员与偏好”设置：

- 管理员姓名/称呼：1–40 个 Unicode 字符，不含控制字符，用于问候与显示，不是第二个登录账号。
- 密码：至少 12 个字符；由用户自行输入并确认，没有预置密码，不会自动设密码或免登录。
- 单管理员凭据以加盐派生值持久化，不保存明文密码；会话在进程内，Cookie 为 HttpOnly、SameSite=Strict，重启后需重新登录。

当前源码的**初始化只允许本机 localhost 请求，即使远程使用 HTTPS 也不开放首次创建**。登录与管理请求要求 localhost 或服务端识别的 HTTPS，以及同源校验。现有凭据文件不可读或损坏时拒绝再次初始化，不要删除它来绕过认证，应从可信备份恢复并核对权限。

无桌面 Linux 服务端可在自己的电脑通过 SSH 本地转发访问。将命令中的主机和账户替换成已配置的实际值：

```sh
ssh -N -L 18080:127.0.0.1:8080 user@server-host
```

然后浏览器打开 `http://localhost:18080/`；SSH 服务必须已正确配置。该方式的服务端连接为 loopback，不需要公开管理员密码或改写认证规则。

### 3.2 三步引导

欢迎页提供“开始配置”和“跳过，我自己配置”，三步为：

1. **管理员与安全**：设置称呼和密码；已有管理员登录并确认称呼。
2. **添加音乐源**：直接使用“设置 → 音乐文件夹”的本地/NAS 管理，连接、检测并扫描，也可选择稍后配置。
3. **连接播放终端**：使用终端中心现有添加向导签发令牌，手动在目标设备安装；确认注册、上线、授权和 DAC 状态。

称呼和步骤保存在服务端；跳过只保存浏览器的隐藏选择，不伪造完成状态。可在“设置 → 管理员与偏好”继续。结束页显示真实源/节点/授权摘要；“开始聆听”只结束引导并进入主页，不自动播放或执行终端安装。已有管理员的旧实例不会被强制重设密码或重走新引导，可补全称呼。

### 3.3 HTTPS/反代限制

当前入口使用 HTTP `ListenAndServe`，不是自带证书配置的 HTTPS 监听器。管理员安全上下文只接受请求自身的 TLS 状态或严格的 localhost 连接，**故意不信任 `Forwarded` / `X-Forwarded-*`**，并核对 `Origin`、Host 与同源关系。

因此，“在 Nginx/Caddy 终止 TLS，再加 `X-Forwarded-Proto: https`”不能直接视为已支持的新认证部署方法。普通 TLS 终止代理仍可能使服务端拒绝登录；容器端口转发也不保证服务端看到 loopback。不要通过伪造 Origin/Host 或关闭校验来迁就部署。

在完整验证代理、Cookie、同源和首次设置流程前，使用服务端本机浏览器/SSH loopback 通道管理。对外 HTTP 与 ZAT 均只限可信网络；内置管理员保护敏感管理接口，但不是所有只读及播放 API 的统一网络认证屏障。

## 4. Linux 服务端

### 4.1 安装

需要 Linux amd64/arm64 + systemd。安装器使用专用非 root 用户 `hifihub-server`；不承诺特定内存、吞吐或终端数量的性能指标。

默认配置监听 `0.0.0.0:8080`，先用防火墙限制入口，或在首次安装前编辑包内 `server.env` 为 `HIFIHUB_LISTEN=127.0.0.1:8080`。已存在的 `/etc/hifihub/server.env` 会保留，须编辑实际配置而非只改模板。

```sh
sudo apt-get update
sudo apt-get install -y gnupg
tar -xzf Zephyr-HiFi-Kit-server-0.58.tar.gz
cd Zephyr-HiFi-Kit-server-0.58
sha256sum -c SHA256SUMS
cd server
sudo ./install.sh
```

安装器按架构安装 `/usr/bin/hifihub-server`，创建 `/var/lib/hifihub-server`，复制分发集到其 `dist/`，安装并启动 `hifihub-server.service`。同目录有 `trust-root.asc` 时可导入公钥；必须先独立核实来源和指纹。

### 4.2 配置与检查

编辑 `/etc/hifihub/server.env`。例如先采用本机管理和手动更新：

```sh
HIFIHUB_LISTEN=127.0.0.1:8080
HIFIHUB_ZAT_ADDR=0.0.0.0:7800
HIFIHUB_DATA_DIR=/var/lib/hifihub-server
HIFIHUB_DIST_DIR=/var/lib/hifihub-server/dist
HIFIHUB_GPG_HOME=/var/lib/hifihub-server/gnupg
HIFIHUB_SIGNATURE_MODE=enforce
HIFIHUB_PLAY_GATE=true
HIFIHUB_IDLE_TIMEOUT=120m
HIFIHUB_UPDATE_SELF=false
HIFIHUB_UPDATE_AUTO=false
HIFIHUB_UPDATE_FLEET=false
```

上述更新值是本指南的保守配置，不是通用代码默认值（见第 9 节）。需要 LAN 终端时选择适当 LAN 绑定并配置防火墙。

```sh
sudo systemctl restart hifihub-server
systemctl status hifihub-server
journalctl -u hifihub-server -f
curl -fsS http://127.0.0.1:8080/api/v1/health
curl -fsSI http://127.0.0.1:8080/dl/agent-install.sh
```

需要原生 FLAC 解码时，使用 `Zephyr-HiFi-Kit-server-0.58-audio-amd64.tar.gz` 或 `Zephyr-HiFi-Kit-server-0.58-audio-arm64.tar.gz`，按包内同样的 `server/install.sh` 安装，并提供该构建链接的 libFLAC 运行库（例如发行版提供的 `libflac12`；以实际 ABI 为准）。管理版与音频版沿用相同内部配置和服务名。

## 5. 音乐源与扫描

在管理员解锁后进入“设置 → 音乐文件夹”。这是**服务端读取**源目录，不是从浏览器上传整个音乐库。

### 5.1 本地目录与 NAS

- **本地目录**：填写服务端已有且可读的绝对路径，例如 Windows 的 `D:\Music`，Linux 的 `/srv/music`。本地“启用/停用”改变源状态，不会挂载或删除物理目录。
- **Windows SMB**：填写主机、共享名及必要凭据；后端通过 WNet 建立当前登录会话的 UNC 连接，不调用 Linux `mount.cifs`。实际访问权由共享/系统账户控制，“只读”选项不能替代 NAS ACL。凭据冲突可能影响同账户到同 NAS 的其他连接，应先核实会话，勿批量断开系统共享。
- **Linux SMB**：CIFS 后端依赖 `cifs-utils` 和挂载权限。默认加固 systemd 服务是非 root，不能假定安装 `cifs-utils` 后即可在控制台挂载。推荐由系统管理员在宿主机预挂载、授予服务账户只读权限，再作为本地源加入；不要为方便挂载而全局关闭沙箱。
- **容器**：宿主机先挂载 NAS，再只读 bind 到容器内路径；不要只把宿主机路径填到容器里，也不要默认使用 privileged。

添加后启用/挂载，检查连接状态，再扫描。检测连接本身不挂载、不卸载；SMB 检测前应已挂载。扫描失败或网络暂时不可用时先排查连接，不要直接删除来源。

### 5.2 复用、增量、归档和删除

- **复用来源**：同一本地实际目录或同一 SMB 主机/共享再次添加时复用源身份，不生成另一份源记录。复用活跃源不表示覆盖原凭据；修改配置请使用编辑功能。
- **增量扫描**：默认按大小与纳秒修改时间跳过未变化曲目，返回新增、更新、未变、移除等计数。移除指索引中不再存在的文件，不删除原文件；同源相对路径使用稳定曲目身份。
- **完全重新扫描**：重新读取文件元数据；不是清空源再重建。更换同大小同时间的内容或需刷新标签时可使用此模式。
- **失败保护**：遍历、连接校验或扫描期间文件检查失败时，不提交这次索引更新；不是以空目录覆盖旧曲库。
- **归档**：保留来源和曲目索引，停止该源播放，已挂载的 SMB 会先卸载，并清除保存的 SMB 用户名/密码/域。重新添加相同来源可复用原身份，需重新连接并扫描；必要时重新填写凭据。
- **删除来源**：删除源及曲目索引，与归档不同；并不删除源磁盘上的音乐。误删会影响曲库引用，应先备份。

格式探测与实际解码分离。WAV、FLAC 等可被识别和编目，并不代表所有构建都能播放它们；MP3/AAC/DSD 等也不能仅因显示在曲库中就宣称可解码。

## 6. Linux / Android 终端

### 6.1 注册与在线安装

先在服务端以管理员身份打开“终端中心 → 添加终端”，签发一次性令牌。**不要使用旧文档中不带会话的 `POST /nodes/token` 示例。** 令牌有有效期且只能使用一次；注册、上线和授权分别确认。

使用向导给出的命令，在所选终端执行。也可先下载、检查脚本再运行（以下地址与令牌需按实际替换）：

```sh
curl -fsS 'http://<服务端LAN地址>:8080/dl/agent-install.sh' -o agent-install.sh
# 检查文件来源与内容后执行；真实播放前确认服务端 dist 已换成音频分发集
sudo bash ./agent-install.sh --server 'http://<服务端LAN地址>:8080' --token '<一次性令牌>'
```

明文 HTTP 安装只适用于可信隔离 LAN；生产应使用已验证的安全传输或通过可信渠道离线传送并核对校验和。不要将不可信网络下载的脚本直接以 root 执行。

安装脚本自动识别 amd64/arm64/armhf，下载 `/dl/hifihub-agent_0.58_<arch>.deb`，通过 dpkg/apt 安装依赖，写入 `/etc/hifihub/agent.env` 并启动 `hifihub-agent`。`--bind` 控制本地控制台绑定（默认 `0.0.0.0`、端口 8080）；`--deb` 可指定本地包。

新终端默认未授权。在控制台授权后再进行播放/软件下发。内置 3 席不需要另行购买许可证，但不会替你逐台授权。

### 6.2 离线包与音频终端

终端安装仍需 Debian/Ubuntu/Raspberry Pi OS 的 apt/dpkg + systemd；完全离线时必须预备依赖包。“离线 deb”不代表可以离线联系不到服务端仍完成注册。

独立管理 deb 的安装示例：

```sh
sudo dpkg -i ./Zephyr-HiFi-Kit-terminal-0.58-linux-arm64.deb
sudo apt-get -f install
```

服务与包查询仍用内部名：

```sh
dpkg -s hifihub-agent
systemctl status hifihub-agent
journalctl -u hifihub-agent -f
```

使用独立音频终端包时解压，进入包含以下文件的目录，核对校验后使用本地 deb：

```sh
sha256sum -c SHA256SUMS
sudo bash ./agent-install.sh \
  --server 'http://<服务端LAN地址>:8080' --token '<一次性令牌>' \
  --deb ./hifihub-agent_0.58_arm64.deb
```

amd64 改用 `hifihub-agent_0.58_amd64.deb`。真实播放需要音频版的 ALSA 后端及其运行库（deb 声明依赖），管理版没有该后端。选择 DAC 后先用短曲验证格式、采样率、音量与实际出声，不能把“在线”当成“播放通过”。

手动安装后需在 `/etc/hifihub/agent.env` 配置服务端 URL、一次性令牌和可选别名，再重启服务。该文件含注册信息，应限制为 root 可读。终端本地控制台以及 root 动作应仅限可信网络，按需要收紧 `HIFIHUB_AGENT_ALLOW_LOCAL`、`HIFIHUB_AGENT_ALLOW_POWER`；优先通过服务端受管理员保护的终端管理入口操作。

### 6.3 Agent 版本检测与直接升级

Agent 每次心跳都会上报版本、架构和构建类型（`audio` 或 `management`）。终端中心列表与详情会显示“当前版本 → 服务端目标版本”和构建类型；版本不一致时可直接点击“升级 Agent”或选择多台后批量升级。

- 服务端按构建类型选择安装包：管理版为 `/dl/hifihub-agent_<版本>_<架构>.deb`，音频版为 `/dl/hifihub-agent_<版本>_<架构>_audio.deb`，不会把音频终端静默换成管理版。
- 升级命令携带 SHA256，终端下载后校验再 dpkg 安装并重启；播放中的终端会等待空闲，播放保护不被绕过。
- 升级保留 `/etc/hifihub/agent.env` 与注册信息；失败会显示原因，可重试或继续用 `dpkg -i` 手动安装。
- 自动收敛策略仍位于“终端中心 → 高级维护”，手动升级不依赖该策略。

要让服务端同时支持音频终端，需要把音频终端包导入分发目录（管理版文件名保持不变，音频版追加 `_audio`）：

```sh
# Linux 服务端
scripts/import-audio-dist.sh /var/lib/hifihub-server/dist \
  Zephyr-HiFi-Kit-terminal-0.58-audio-arm64.tar.gz
```

```powershell
# Windows 服务端
.\import-audio-dist.ps1 -DistDir C:\path\to\dist `
  -Bundle C:\path\to\Zephyr-HiFi-Kit-terminal-0.58-audio-arm64.tar.gz
```

导入脚本会校验包内 SHA256SUMS 并重建分发目录的 SHA256SUMS。若终端提示“服务端分发缺少音频包”，先导入对应架构的音频终端包，再重试升级。不要为了省事把管理版 deb 推给需要 ALSA 播放的终端。

### 6.4 Android

安装 `Zephyr-HiFi-Kit-terminal-0.58.apk`，仅在信任安装包来源后允许系统安装未知应用。当前目标为 Android API 26+ 的 arm64-v8a / armeabi-v7a；填写服务端可达地址、一次性令牌与可选别名后启动，回到服务端确认在线并授权。

Android 输出使用 AAudio，优先独占、不可用时回退共享；设备及系统可能改变实际音频路径，不能无条件保证比特完美。Android 不使用 ALSA 设备名，也不通过 Linux deb 自动收敛升级。APK 更新需保留兼容签名，不能假定任意重签 APK 可覆盖原应用。

## 7. 公钥、授权与软件下发

生产使用 `HIFIHUB_SIGNATURE_MODE=enforce`。GPG 缺失、公钥不可信、制品缺签名或签名不匹配时，应修正依赖/签名，而不是关闭校验。

- 主密钥保存在离线环境，签名子密钥留在受控签名主机；服务端只导入用于验证的公钥。
- 在“设置 → 授权信息”导入并核对公钥信任根；上传制品时同时提供 detached 签名。
- 离线许可证由签发方交付，服务端验证后扩容。默认内置 3 席，超过该数量才需要更大席位授权；管理员登录与许可证授权互不替代。
- 软件交付需匹配终端操作系统与架构，不能只看 amd64/arm64。不要把 Windows 或 macOS 文件作为 Linux 可安装包。
- 签名制品/BOM 下发与 APT 源安装是不同路径。APT 源需按实际发行版和来源配置，不能把 APT 安装说成所有包都已在本地仓库做过 detached GPG 验签。
- 软件安装、系统更新与版本收敛有播放保护/任务门控；实际失败仍应检查状态、日志和回滚结果，不承诺永远不会中断。

常规管理操作建议从已登录控制台完成。调用受保护 REST 时还需会话 Cookie、同源 `Origin` 和合适的 Content-Type；仅复制旧版匿名 curl 请求不再等价于管理操作。

## 8. NAS / 容器

镜像入口：

```sh
docker pull ghcr.io/eyebright/zephyr-hifi-kit-server:0.58
```

可参考仓库 `packaging/docker/compose.yml`，核对镜像标签为 `0.58`。关键部署要点：

- 持久数据挂到 `/data`，分发集挂到 `/data/dist`；镜像中的服务用户为 `hifihub-server`（UID/GID 1000），宿主目录须授予必要权限，勿对整个宿主目录放宽权限。
- 真实播放使用音频版终端分发集。服务端镜像包含 Linux libFLAC 解码能力，不意味着挂入的管理版终端 deb 有 ALSA。
- 宿主预挂载 NAS，再以只读 bind 暴露给容器；在控制台添加容器内本地路径。
- HTTP 8080 与 ZAT TCP 7800 分别限制到可信网络；不需要公网直连，也不建议 privileged。
- 容器通过更换镜像并保留 `/data` 升级，`HIFIHUB_UPDATE_SELF=false`、`HIFIHUB_UPDATE_AUTO=false`。终端收敛可单独启用，但先确认分发集与镜像版本、音频能力一致。

**新管理员初始化边界仍适用。** 将容器端口发布到宿主的 `127.0.0.1`，不必然使容器内收到的连接为 loopback；普通 TLS 反代也不自动满足认证要求。在验证容器内部 loopback 初始化与后续安全登录路径之前，不应把镜像健康检查或端口映射成功算作容器管理完整验收。

## 9. 备份、升级与卸载

### 9.1 音乐数据备份不是整机备份

“设置 → 备份与维护”的音乐数据导出包括源配置的非凭据部分、曲目/稳定身份、封面、播放列表、播放历史与统计。它不包含原始音乐文件、SMB 密码、管理员凭据、终端密钥、运行队列及完整服务端配置，也不能当作包含所有用户状态的迁移镜像。

恢复/清理需管理员确认。恢复前停止所有播放（包括暂停会话）、结束扫描、卸载 SMB，并先备份当前数据；恢复后重新确认路径、凭据和连接，不自动挂载共享。

完整迁移应在停止服务端后备份整个数据目录、独立配置、外置数据路径以及要保留的分发集；音乐文件单独备份。完整数据库可能含 SMB 凭据与节点密钥，备份须按敏感数据保管，不能上传到 Release 或公开仓库。

### 9.2 从 0.4.3 升到 0.58

**必须手动安装一次。** 0.4.3 的自动更新器硬编码旧发布包名，不会因为 0.58 改了资产命名就自动找到新包。

- **Windows**：先停止播放，在前台窗口按 Ctrl+C 并确认服务端进程退出；完整备份后，将新 ZIP 解压到独立目录，替换程序和正确的分发集，保留原数据目录并重新设置 `HIFIHUB_DATA_DIR`。启动后核对版本、登录、源、索引和终端状态。不要让两个进程同时使用同一数据库，也不要用新的空 data 覆盖原 data。
- **Linux**：`sudo systemctl stop hifihub-server` 后完整备份，解压 0.58 匹配类型的包，运行 `server/install.sh`。安装器保留既有 env，但会复制程序与分发集，须核对自定义路径和音频终端文件。启动后检查日志与管理员登录。
- **容器**：停止容器并备份数据卷，换用 `ghcr.io/eyebright/zephyr-hifi-kit-server:0.58` 重建，保留原卷与路径，并刷新相应分发集。
- **终端**：包内部名不变，可用新 deb 手动升级；确认服务端分发集后再使用终端收敛。Android 通过兼容签名 APK 更新。

有问题需回退时保留升级后的数据，优先恢复升级前完整备份和匹配程序，不能假定新数据库可安全配旧二进制。

### 9.3 更新开关

| 环境变量 | 通用代码默认 | Windows 启动脚本 | 说明 |
|---|---|---|---|
| `HIFIHUB_UPDATE_SELF` | `true` | `false` | 发布检查/服务端自升级开关 |
| `HIFIHUB_UPDATE_AUTO` | `true` | `false` | 检测更新后自动安装 |
| `HIFIHUB_UPDATE_FLEET` | `true` | `false` | Linux 终端向当前服务端版本收敛 |
| `HIFIHUB_UPDATE_INTERVAL` | `10m` | 同通用值 | 轮询间隔 |
| `HIFIHUB_UPDATE_MAX_PARALLEL` | `2` | 同通用值 | 并行升级上限 |
| `HIFIHUB_UPDATE_MAX_ATTEMPTS` | `3` | 同通用值 | 失败挂起阈值 |
| `HIFIHUB_UPDATE_RING_PERCENT` | `20` | 同通用值 | 每轮灰度比例 |

`HIFIHUB_UPDATE_FEED` 可指定发布源；默认使用现有项目 Releases。Linux 自升级涉及资产选择、摘要/包内校验、可选发布签名验证、文件替换和重启权限。默认非 root 加固服务并不自动拥有覆盖 `/usr/bin` 和重启服务的权限，不要把默认 `true` 解读为已保证可完成升级。

收敛策略也可能已持久化在数据库中，迁移后应在控制台核对实际策略与分发文件。Windows 不承诺自更新；按需启用 fleet 时，仍须先检查授权、平台支持、音频包类型、版本和校验清单。播放中、离线、缺包或连续失败的节点不应视作已升级；以回连心跳版本为准。

### 9.4 卸载

Linux 服务端使用 0.58 包内 `server/uninstall.sh`，保留配置和数据；终端包仍使用内部名：

```sh
sudo apt-get remove hifihub-agent
```

Windows 停止进程后可移除程序目录，数据目录按需要保留并先备份。卸载程序不等于服务端注销终端；如不再使用该设备，应在控制台处理注销和席位释放。

## 10. 排障与逐步验证

| 现象 | 核查 |
|---|---|
| HTTP 无响应 | 进程日志、端口占用、`HIFIHUB_LISTEN`；Windows 用 `Invoke-RestMethod`，Linux 查 systemd/journal |
| 远程无法初始化/登录 | 初始化仅 localhost；普通 HTTP 拒绝管理；检查第 3 节 TLS/反代与 Origin 限制，不伪造头 |
| 管理操作 401/403 | 会话失效、未登录、访问不是安全上下文或同源；重启后重新登录 |
| 登录 429 | 命中登录退避，等待后检查密码，不循环重试 |
| 启动后像新实例 | 检查 `HIFIHUB_DATA_DIR`、`HIFIHUB_DB_PATH` 是否仍指向原目录，不要重新初始化覆盖旧数据 |
| `/dl/agent-install.sh` 404 | `HIFIHUB_DIST_DIR` 路径或文件缺失，Windows 应指包内 dist 或明确配置的音频分发集 |
| FLAC 已入库但无法播放 | 区分格式识别和解码；核查所用构建是否有 Linux libFLAC 后端；外部 ffmpeg 不代表自动回退已实现 |
| 终端在线但无声 | 管理终端没有 ALSA 后端；核对音频包、授权、ZAT 7800、DAC、格式与音量 |
| Windows NAS 失败 | 同一登录账户的网络会话、共享权限、用户名/域、凭据冲突；不要批量清除所有共享 |
| Linux NAS 失败 | cifs-utils、挂载权限和 systemd 沙箱；可改用宿主预挂载目录作为本地源 |
| 扫描失败 | 查看返回错误，先恢复连接/读权限；失败扫描保留旧索引，不要为“刷新”删除源 |
| `artifact_rejected` / `license_rejected` | GPG 可执行程序、可信公钥、签名与文件对应关系；保持 enforce |
| 终端未授权 / 席位不足 | 逐台授权；内置 3 席无需激活额外许可证，超过可用席位才需扩容或释放 |
| 升级等待/失败 | 播放门控、在途任务、签名/校验、架构、运行权限、dist 类型；0.4.3 跨到 0.58 手动安装 |

建议在真实部署上分别记录：健康检查、管理员初始化与锁定/重登录、源复用、增量与完整扫描、归档/恢复、终端注册授权、WAV/FLAC 适用路径、实际出声及备份恢复。一次健康检查或单元测试不能代替这组端到端验证。

## 11. 源码构建与测试

使用满足各 `go.mod` 的 Go 工具链（当前声明 `go 1.27.4`）。管理构建使用纯 Go；音频构建需相应 C 工具链。所有示例明确固定 `0.58`：

```sh
# 在仓库根目录：管理发布构建
VERSION=0.58 sh scripts/build-release.sh

# 单元与前端测试
go test ./...
go -C agent test ./...
node --test internal/web/*.test.cjs
```

Linux 原生音频构建（发布构建会调用音频构建；以下两种方式二选一，不要在同一输出目录连续重复构建）：

```sh
sudo apt-get install -y pkg-config libflac-dev libasound2-dev flac
VERSION=0.58 AUDIO=1 TEST=1 sh scripts/build-release.sh
# 或仅构建音频产物和音频终端包并测试：
# VERSION=0.58 TEST=1 sh scripts/build-audio.sh
```

构建脚本拒绝覆盖已有的非空版本输出目录；重建前请先保留并移走旧输出，不要直接删除可能需要的发布产物。前端测试还需安装支持 `node --test` 的 Node.js。

`build-audio-cross.sh` 提供 ARM 交叉构建路径，需自行准备匹配的交叉编译器与目标库；armhf 管理包的存在不表示本次提供了同名 armhf 音频发布包。

Android 构建脚本使用 Linux NDK 工具链布局，需 Android SDK、NDK（脚本指定版本）、JDK 和 Gradle；配置好 `ANDROID_HOME` / `ANDROID_NDK_HOME` 后：

```sh
VERSION=0.58 sh scripts/build-android.sh
```

签名材料由构建环境安全提供，不写入源码或发布包。Windows/macOS 上是否可调整工具链另行验证，不将脚本当前 Linux 限制宣称为平台永远无法构建。

仓库包含管理员/源生命周期/前端引导测试、ALSA/FLAC 音频测试及冒烟脚本；测试存在不代表本次已执行，更不代表新认证下 Windows 黄金路径已实测。本次文档以 `internal/api/settings_admin.go`、`internal/api/api.go`、`internal/library/`、`internal/decode/`、`internal/mount/`、`internal/web/assets/experience.js`、`agent/internal/player/`、`packaging/server/start.ps1`、`scripts/build-release.sh` 及相关部署脚本中的实际实现为依据。

官网：https://www.zephyr.hk · 联系：services@zephyr.hk · MIT [LICENSE](LICENSE)
