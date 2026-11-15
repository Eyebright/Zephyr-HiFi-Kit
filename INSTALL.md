# Zephyr Music Player — 安装与部署说明

版本 0.3.0 · 服务端 x86_64 / aarch64 Linux · 终端 Zephyr Music Player（Debian / 树莓派）

本包交付**已编译、已测试**的完整确认功能集：

- **服务端**：唯一下发源（应用目录 / 制品仓库 / GPG 强制验签 / BOM 期望态 / 任务状态机 /
  播放门控 / 回滚）、节点注册与配置（分组 / 备注 / 播放保护 / 离线判定）、许可证与席位授权、
  终端命令通道（系统更新 / 网络诊断）、灰度发布环、审计日志、告警、密钥管理、终端控制台
  反向代理、终端安装包自助下载（`/dl/`）。
- **终端 Agent**：指标采集 / DAC 占用探测与仲裁 / ALSA 枚举 / systemd 白名单控制 / 电源 /
  apt·dpkg 执行器 / 命令执行（系统更新检查与升级、网络诊断）/ 本地 Web 控制台。
- **内嵌管理控制台**：总览 / 终端管理 / 应用仓库 / 服务下发 / 密钥与授权 / 审计 + 告警抽屉。

可行性审计后**已删减**（不在本包内，详见 README.md）：Roon 包重签名/ingest（专有签名不可
合法再分发）、服务端在线构建流水线（工具链攻击面）、二进制补丁（破坏签名链）、远程 .sh
拉取安装（SSRF + 未签名代码）、Agent 自更新、控制台角色切换（鉴权交由反向代理）。

官方联系：services@zephyr.hk · https://www.zephyr.hk

---

## 1. 包内容

```
zephyr-music-server-0.3.0/
├── server/
│   ├── hifihub-server-linux-amd64      # x86_64 服务端单文件（内嵌管理控制台）
│   ├── hifihub-server-linux-arm64      # aarch64 服务端单文件
│   ├── install.sh                      # 安装器（二进制 + systemd + 配置 + 信任根 + dist）
│   ├── uninstall.sh                    # 卸载器（保留配置与数据）
│   ├── server.env                      # 配置模板（enforce 模式）
│   ├── hifihub-server.service          # systemd 单元（非 root 运行 + 沙箱加固）
│   └── dist/                           # 终端自助下载目录（由 /dl/ 白名单暴露）
│       ├── hifihub-agent_0.3.0_amd64.deb
│       ├── hifihub-agent_0.3.0_arm64.deb
│       ├── hifihub-agent_0.3.0_armhf.deb
│       ├── agent-install.sh            # 终端一键安装脚本
│       └── SHA256SUMS
├── agent/
│   ├── hifihub-agent_0.3.0_amd64.deb   # x86_64 终端
│   ├── hifihub-agent_0.3.0_arm64.deb   # 树莓派 3/4/5 64 位、ARM 服务器
│   └── hifihub-agent_0.3.0_armhf.deb   # 树莓派 2/3/4 32 位（ARMv7）
├── INSTALL.md                          # 本文件
└── SHA256SUMS                          # 全部文件校验和
```

> 引擎二进制沿用内部名 `hifihub-server` / `hifihub-agent`；产品对外名为 Zephyr。
> 服务端与终端的配置/数据目录互不冲突（`/var/lib/hifihub-server` vs `/var/lib/hifihub`）。

校验完整性：

```sh
cd zephyr-music-server-0.3.0
sha256sum -c SHA256SUMS
```

---

## 2. 资源需求

| | 服务端 | 终端 Agent |
|---|---|---|
| 目标系统 | x86_64 / aarch64 Linux + systemd | Debian / Raspberry Pi OS + systemd |
| 磁盘 | 二进制 ~12MB + SQLite + 制品缓存 + dist（.deb 为主要占用） | 二进制 ~9MB / deb ~4MB + 状态 JSON |
| 内存 | 典型 ~20–60MB；单元硬上限 `MemoryMax=512M` | 典型 ~15–40MB；硬上限 `MemoryMax=120M` |
| CPU | 空闲近 0（15s 清扫定时器） | 定时器 15s/10s/10s/10min + /proc 扫描，<1% |
| 依赖 | 纯 Go 无 CGO；**enforce 需 `gnupg`**；SMB 挂载需 `cifs-utils`；音频版另需 `libflac8` | root + systemd + apt/dpkg + alsa-utils；音频版另需 `libasound2` |
| 权限 | 专用非 root 用户 `hifihub-server` | root（动作受白名单限制，NFR-S5） |

单台 modest x86 盒子即可管理数百终端；吞吐瓶颈为单写 SQLite，对家庭/SMB 机群足够。

---

## 3. 服务端安装（x86 Linux）

### 3.1 安装

```sh
# 安装 gpg（enforce 模式必需）
sudo apt-get update && sudo apt-get install -y gnupg

# 解压后进入 server/ 目录运行安装器
tar -xzf zephyr-music-server-0.3.0.tar.gz
cd zephyr-music-server-0.3.0/server
sudo ./install.sh
```

安装器会：按架构选择二进制装到 `/usr/bin/hifihub-server`；创建非 root 用户
`hifihub-server`；写入 `/etc/hifihub/server.env`（已存在则保留）；创建
`/var/lib/hifihub-server/{artifacts,gnupg,dist}`；把 `dist/`（终端 deb + 安装脚本 +
SHA256SUMS）装到 `/var/lib/hifihub-server/dist`；安装并启用 systemd 单元；若同目录存在
`trust-root.asc` 则自动导入信任根公钥。

### 3.2 配置

编辑 `/etc/hifihub/server.env`（时长用 Go 语法 `15s`/`10m`/`120m`）：

```sh
HIFIHUB_LISTEN=0.0.0.0:8080
HIFIHUB_DATA_DIR=/var/lib/hifihub-server
HIFIHUB_DIST_DIR=/var/lib/hifihub-server/dist
HIFIHUB_SIGNATURE_MODE=enforce      # 生产必须 enforce（NFR-S2）
HIFIHUB_PLAY_GATE=true              # FR-3.10 播放门控
HIFIHUB_IDLE_TIMEOUT=120m
```

改后重启：`sudo systemctl restart hifihub-server`

### 3.3 验证

```sh
curl -s http://127.0.0.1:8080/api/v1/health
# {"status":"ok","service":"hifihub-server","signature_mode":"enforce","play_gate":true}

curl -s http://127.0.0.1:8080/metrics/server        # 服务端自身消耗（根路径，非 /api/v1）
curl -sI http://127.0.0.1:8080/dl/agent-install.sh    # 终端安装脚本可下载（200）

systemctl status hifihub-server
journalctl -u hifihub-server -f
```

浏览器打开 `http://<服务端IP>:8080/` 进入内嵌管理控制台（总览 / 终端管理 / 应用仓库 /
服务下发 / 密钥与授权 / 审计）。

> **控制台鉴权**：本包控制台不含登录页（角色切换已删减）。生产部署**必须**在前面加
> Nginx/Caddy 做 TLS 终止 + Basic Auth / OIDC / mTLS，否则任何能访问 8080 的人都可管理机群。

---

## 4. enforce 模式：GPG 签名信任根（关键安全步骤）

enforce 模式下，**未签名制品会在入库时被拒绝**。服务端只用**公钥**验签，绝不持有私钥。
密钥分工遵循安全不变量：

- **主密钥（master）**：离线保管，仅用于生成/认证子密钥与签发许可证，永不联网、不上服务器。
- **签名子密钥（signing subkey）**：在受控的构建/签名主机上，为每个制品生成 detached 签名。
- **公钥（信任根）**：导入服务端 `GNUPGHOME`，作为验签信任根；控制台「密钥与授权」页也可
  在线粘贴 armored 公钥导入并设为信任根（`POST /api/v1/keys/import` →
  `POST /api/v1/keys/{id}/trust-root`，导出用 `GET /api/v1/keys/trust-root.asc`）。

### 4.1 离线主机：生成密钥并导出信任根

```sh
# 主密钥（认证用，离线机器上执行）
gpg --quick-generate-key "Zephyr Release Signing <services@zephyr.hk>" ed25519 cert never
# 记下输出的指纹 FPR，添加签名子密钥（有效期 2 年）
gpg --quick-add-key <FPR> ed25519 sign 2y
# 导出公钥信任根
gpg --armor --export <FPR> > trust-root.asc
```

把 `trust-root.asc` 放到 `server/` 目录后运行 `install.sh` 会自动导入；或手动导入：

```sh
sudo GNUPGHOME=/var/lib/hifihub-server/gnupg gpg --batch --import trust-root.asc
sudo chown -R hifihub-server:hifihub-server /var/lib/hifihub-server/gnupg
sudo systemctl restart hifihub-server
```

### 4.2 签名主机：为制品生成 detached 签名

```sh
gpg --batch --yes --armor --detach-sign \
    --local-user <SIGNING_SUBKEY_FPR> \
    -o shairport-sync_4.3.2_arm64.deb.asc shairport-sync_4.3.2_arm64.deb
```

### 4.3 上传已签名制品（服务端入库时强制验签）

```sh
curl -s -X POST http://<服务端IP>:8080/api/v1/apps/shairport-sync/versions \
  -F 'meta={"version":"4.3.2","arch":"arm64","format":"deb","units":["shairport-sync.service"],"occupies_dac":true,"lifecycle":"available"}' \
  -F 'artifact=@shairport-sync_4.3.2_arm64.deb' \
  -F 'signature=@shairport-sync_4.3.2_arm64.deb.asc'
```

验签失败或缺少 `.asc` 时返回 `artifact_rejected`，文件不会落盘。手动复核签名：

```sh
GNUPGHOME=/var/lib/hifihub-server/gnupg gpg --verify app.deb.asc app.deb
```

> 终端 Agent 下载制品后校验 **SHA256 完整性**；GPG 验签在**服务端入库**这一唯一入口强制
> 执行（服务端是唯一下发源，FR-3）。

### 4.4 离线签发许可证（授权中心）

许可证是**离线签发**的 JSON + detached GPG 签名，服务端只验签、不签发。未激活许可证时
控制台可浏览，但**下发任务与终端授权被门控**。

在离线主机上：

```sh
cat > license.json <<'JSON'
{"key":"ZMP-2026-XXXX-XXXX","account":"Acme HiFi","seats":10,"expires_at":1893456000}
JSON

gpg --batch --yes --armor --detach-sign --local-user <FPR> \
    -o license.json.asc license.json

# 分别取 base64（单行）
base64 -w0 license.json      # → payload_b64
base64 -w0 license.json.asc  # → sig_b64
```

激活（也可在控制台「密钥与授权」页粘贴两段 base64）：

```sh
curl -s -X POST http://<服务端IP>:8080/api/v1/license/activate \
  -H 'Content-Type: application/json' \
  -d '{"payload_b64":"<payload>","sig_b64":"<sig>"}'

curl -s http://<服务端IP>:8080/api/v1/license
# {"active":true,"key":"ZMP-…","account":"Acme HiFi","seats":10,"used":0,"expires_at":…}
```

签名必须能被信任根验证，否则返回 `license_rejected`。席位用尽后新终端授权会被拒绝
（`seat_limit`），可注销闲置终端释放席位。

---

## 5. 终端 Agent 安装（Zephyr Music Player）

### 5.1 一键安装（推荐，各架构通用）

**第一步**：服务端签发一次性注册令牌（控制台「终端管理 → 添加终端」向导第 1 步会直接
生成并展示下面的命令；等价 REST）：

```sh
curl -s -X POST http://<服务端IP>:8080/api/v1/nodes/token \
  -H 'Content-Type: application/json' -d '{"ttl_seconds":1800}'
# {"token":"<一次性令牌>","expires_at":<unix秒>}
```

**第二步**：在终端上执行（x86_64 / 树莓派 64 位 / 32 位均适用，脚本按 `uname -m` 自动选包）：

```sh
curl -fsSL http://<服务端IP>:8080/dl/agent-install.sh | sudo bash -s -- \
  --server http://<服务端IP>:8080 --token <一次性令牌>
```

脚本会：按 `uname -m` 下载对应架构 deb（`/dl/hifihub-agent_0.3.0_<arch>.deb`）→
`dpkg -i` + `apt-get -f install` → 写入 `/etc/hifihub/agent.env`（SERVER_URL / TOKEN /
LOCAL_BIND，保留其它自定义键，0600 root 属主）→ 启用并启动 `hifihub-agent`。
重复执行安全（幂等）。可选参数：

| 参数 | 说明 |
|---|---|
| `--server <URL>` | 服务端地址（必填） |
| `--token <TOKEN>` | 一次性注册令牌（必填） |
| `--bind <ADDR>` | 本地控制台绑定地址（默认 `0.0.0.0`，端口固定 8080） |
| `--deb <PATH>` | 使用本地 deb（离线安装，跳过下载） |

向导第 3 步会轮询 `GET /api/v1/nodes/token/<token>` 直到终端注册成功，随后自动进入
终端配置（分组 / BOM / 播放保护 / 授权）。

### 5.2 离线安装（无网络直连服务端时）

把 `agent/hifihub-agent_0.3.0_<arch>.deb` 拷到终端：

```sh
sudo dpkg -i hifihub-agent_0.3.0_arm64.deb
sudo apt-get -f install -y        # 修复依赖（alsa-utils 等）
```

deb 会安装 `/usr/bin/hifihub-agent`、systemd 单元、`/etc/hifihub/agent.env`（conffile），
并自动启用服务。编辑 `/etc/hifihub/agent.env`：

```sh
HIFIHUB_AGENT_SERVER_URL=http://<服务端IP>:8080
HIFIHUB_AGENT_TOKEN=<一次性令牌>
HIFIHUB_AGENT_ALIAS=客厅音箱
HIFIHUB_AGENT_LISTEN_ADDR=0.0.0.0:8080
HIFIHUB_AGENT_ALLOW_POWER=true
HIFIHUB_AGENT_ALLOW_LOCAL=true
```

重启后 Agent 自动注册（消费一次性令牌 + machine-id），持久化 NodeID 与**节点密钥
（node secret）**，随后开始心跳（10s）与命令轮询（10s）：

```sh
sudo systemctl restart hifihub-agent
journalctl -u hifihub-agent -f
```

浏览器打开 `http://<终端IP>:8080/` 进入终端本地控制台（性能 / 服务卡片 / DAC / 电源 /
系统更新 / 网络诊断）；服务端控制台「终端管理 → 终端界面」通过反向代理内嵌同一界面。

> - 令牌一次性：注册成功后即失效，重注册需重新签发。
> - **节点密钥**：注册时服务端下发 secret，本地控制台的写操作（安装/卸载/电源/系统升级）
>   必须携带 `X-Node-Secret`；服务端代理会自动注入，直接访问终端本地页面时写操作被拒绝，
>   只读不受影响。
> - `ALLOW_POWER`/`ALLOW_LOCAL` 可按终端收紧（关闭电源/本地安装权限）。
> - 新注册终端默认**未授权**：需在控制台「终端管理」或「密钥与授权」中授权（占用一个
>   许可证席位）后才能接收下发任务。

### 5.3 安卓终端（APK）

安卓终端是独立的 ZAT 播放终端：注册上线、心跳遥测、接收服务端 ZAT 推流并经
**AAudio 比特完美**出声，前台播放服务保活，本地控制台支持暂停 / 停止 / seek / 音量。

**设备要求**

- Android 8.0（API 26）及以上，arm64-v8a 或 armeabi-v7a 设备。
- 与服务端同一可达网络；服务端地址形如 `http://<服务端IP>:8080`。

**安装三步**

1. 从 Release 下载 `zephyr-terminal-0.3.0.apk`（控制台「添加终端」向导架构选 Android
   后也给出同款下载链接），在系统设置允许「安装未知应用」后安装。
2. 启动「Zephyr 终端」，填写服务端地址（`http://<服务端IP>:8080`）、注册令牌（控制台
   「终端管理 → 添加终端」向导、架构选 **Android** 签发的一次性令牌）与可选别名。
3. 点「启动」后终端自动注册并上线，出现在服务端控制台「终端管理」列表（架构 android）。

> - 令牌一次性，注册成功后失效；节点密钥持久化在应用私有目录（0600）。
> - 安卓终端经 AAudio 默认输出出声，无 ALSA 设备名；EXCLUSIVE 不可用时自动回退 SHARED。
> - 新注册终端默认未授权，需在控制台授权（占一个许可证席位）后才能接收播放推送。
> - APK 由 CI 用 NDK r26 交叉编译 Go c-shared 核心（`libzatagent.so`）+ Gradle 组装；
>   配置 release keystore（`ANDROID_KEYSTORE_*` secrets）后为 release 签名，否则 debug
>   签名（仍可安装）。从源码重建见 §10.2。

---

## 6. 首次下发流程（端到端）

以向「客厅音箱」下发 AirPlay 渲染器为例。推荐用服务端控制台「服务下发」工作区操作；
以下给出等价 REST。

```sh
S=http://<服务端IP>:8080/api/v1

# 0) 激活许可证（见 4.4）并授权终端（占一个席位）
curl -s -X POST $S/nodes/<NodeID>/authorize

# 1) 创建应用（目录条目）
curl -s -X POST $S/apps -H 'Content-Type: application/json' \
  -d '{"app_id":"shairport-sync","name":"AirPlay","category":"renderer","upstream":"shairport-sync","license":"GPL"}'

# 2) 上传已签名制品（见 4.3，enforce 验签）

# 3) 创建 BOM 期望态（声明要装哪些应用）
curl -s -X POST $S/boms -H 'Content-Type: application/json' \
  -d '{"bom_id":"livingroom","role":"renderer","apps":[{"app_id":"shairport-sync","version":"4.3.2"}],"variables":{"airplay_name":"客厅音箱","dac_device":"hw:1,0"}}'

# 4) 校验并发布 BOM（版本号从返回的 version 字段取，首个为 1）
curl -s -X POST $S/boms/livingroom/versions/1/validate
curl -s -X POST $S/boms/livingroom/versions/1/publish

# 5) 下发任务（node_id 从终端管理页或 GET /nodes 取）
curl -s -X POST $S/jobs -H 'Content-Type: application/json' \
  -d '{"node_id":"<NodeID>","bom_id":"livingroom","bom_version":1,"kind":"provision"}'

# 6) 跟踪任务状态机
curl -s $S/jobs/<JobID>
```

任务依次经过 `precheck → resolve_deps → waiting_idle → downloading → verifying →
installing → rendering_config → enabling_units → validating → done`。**播放门控**
（FR-3.10）会在终端正在播放时停在 `waiting_idle`，直到空闲或超时，绝不中断播放。
失败时按状态机回滚（`rollback → failed_rolledback`）。终端 Agent 的任务轮询（10s）
会自动领取并执行任务，逐步上报 phase。

下发前置门控：许可证已激活 + 终端已授权 + （终端开启播放保护时）终端空闲。

**系统更新 / 网络诊断走命令通道**（不占用任务状态机）：

```sh
# 控制台或 REST 触发
curl -s -X POST $S/nodes/<NodeID>/commands -H 'Content-Type: application/json' \
  -d '{"type":"os-check"}'                      # 或 os-upgrade / netdiag
# Agent 每 10s 领取：GET $S/nodes/<NodeID>/commands?status=pending
# 执行后回报：POST $S/commands/<CmdID>/result
# 结果查询：
curl -s $S/nodes/<NodeID>/osupdates             # 可升级包列表 + 是否需重启
curl -s $S/nodes/<NodeID>/netdiag               # DNS/TCP/mDNS/SSDP/组播探测
```

`os-upgrade` 同样受播放保护门控（可在终端配置中关闭 `play_protect`，或设置
`auto_os_update` + 维护窗口 `os_window` 让 Agent 在窗口内自动升级）。

四个渲染器及其助手已内置配置模板：

| 应用 | 协议 | 助手 | 关键配置 |
|---|---|---|---|
| `roonbridge` | RAAT | — | 设备/音量在 Roon Core 配置 |
| `networkaudiod` | NAA | — | 输出由 HQPlayer 服务端指定 |
| `shairport-sync` | AirPlay | `nqptp`, `avahi-daemon` | `airplay_name` / `dac_device` / `port` |
| `upmpdcli` | QPlay/DLNA | `mpd` | `room` / `port` / `dac_device` |

---

## 7. 安全不变量（务必遵守）

- **NFR-S2**：生产 `HIFIHUB_SIGNATURE_MODE=enforce`，未签名制品拒绝入库。`dev` 仅限本机
  首次跑通，**严禁用于生产**。
- **密钥分工**：主密钥离线；签名子密钥在受控签名主机；服务端只持公钥信任根，永不持私钥。
  许可证同样离线签发、服务端只验签。
- **NFR-S5**：终端 Agent 以 root 运行，但 systemd 动作受**单元白名单**限制，电源动作受
  `ALLOW_POWER` 门控，本地写操作受 `ALLOW_LOCAL` + 节点密钥门控；不可执行白名单外的单元。
- **唯一下发源（FR-3）**：所有制品只来自服务端；终端不自行从公网拉取应用。`/dl/` 只暴露
  白名单文件名（agent deb / 安装脚本 / SHA256SUMS），不做目录穿越。
- **授权门控**：未激活许可证或未授权终端不能接收下发任务；席位上限由许可证约束。
- **传输安全**：生产请在服务端前加 TLS 反向代理并配置控制台鉴权；注册令牌一次性、短时效。
- **审计**：所有管理变更写入审计日志（控制台「审计」页可筛选并导出 CSV/JSON）。

---

## 8. 排障

| 现象 | 排查 |
|---|---|
| `health` 无响应 | `systemctl status hifihub-server`；`journalctl -u hifihub-server -n 50`；检查 8080 端口/防火墙 |
| 上传返回 `artifact_rejected` | enforce 模式缺 `.asc` 或验签失败；确认信任根已导入 `GNUPGHOME`、签名用对应子密钥 |
| `license_rejected` | 许可证签名无法被信任根验证，或 JSON 字段缺失；用离线主/子密钥重签并核对 base64 单行无换行 |
| 下发被拒 `license_required` / `not_authorized` / `seat_limit` | 先激活许可证（4.4），再授权终端；席位满时注销闲置终端 |
| `/dl/agent-install.sh` 404 | `HIFIHUB_DIST_DIR` 指向的目录缺文件；重跑 `install.sh` 或手动把 `server/dist/*` 拷入 |
| 终端不上线 | 检查 `agent.env` 的 `SERVER_URL`/`TOKEN`；令牌是否过期/已用；`journalctl -u hifihub-agent -f` |
| 命令一直 `pending` | Agent 命令轮询未运行（版本过旧/服务未起）；`systemctl status hifihub-agent`，确认 deb 为 0.3.0 |
| 任务停在 `waiting_idle` | 终端正在播放（播放门控生效）；等待空闲、关闭该终端播放保护，或调小 `HIFIHUB_IDLE_TIMEOUT` |
| 终端界面代理 502 | 终端离线或本地 8080 未监听；核对节点 IP 与 `HIFIHUB_AGENT_LISTEN_ADDR`，检查终端防火墙 |
| 本地控制台写操作 403 | 直连终端页面缺少 `X-Node-Secret`（设计如此）；从服务端控制台「终端界面」操作，或收紧 `ALLOW_LOCAL` |
| 安装失败 `dpkg -i` | 终端缺依赖；执行器会自动 `apt-get -f install` 修复一次；检查终端 apt 源可达 |
| 权限/沙箱报错 | 服务端以非 root 运行，确认 `/var/lib/hifihub-server` 属主为 `hifihub-server` |

资源与日志：服务端 `journalctl -u hifihub-server`；终端 `journalctl -u hifihub-agent`。
两者内存均有 systemd 硬上限（512M / 120M），异常增长会被限制并重启。

---

## 9. 卸载

服务端（保留配置与数据，可安全重装）：

```sh
cd zephyr-music-server-0.3.0/server
sudo ./uninstall.sh
```

终端：

```sh
sudo apt-get remove hifihub-agent          # 停止并禁用服务
sudo apt-get purge hifihub-agent           # 同时清除 conffile
```

在服务端控制台注销终端会释放许可证席位；注销后 30 天内可在「终端管理」中恢复。

---

## 10. 从源码重建（可选）

需要 Go 1.27+（纯 Go，`CGO_ENABLED=0`，无需 dpkg-deb）：

```sh
sh scripts/build-release.sh                # 产出 release/ bundle + tarball + SHA256SUMS
VERSION=0.3.0 sh scripts/build-release.sh  # 指定版本号
```

脚本交叉编译服务端（linux amd64/arm64）与终端（amd64/arm64/armhf），用内置 `debpack`
工具生成三架构 deb，渲染 `agent-install.sh`（替换 `@VERSION@`），组装 `server/dist/`
与发布包。两模块单元测试：`go test ./...`（服务端）与 `cd agent && go test ./...`（终端），
均应全绿。

端到端运行时冒烟测试（需 go / gpg / curl）：

```sh
sh scripts/smoke-test.sh
```

它临时生成 GPG 密钥、以 enforce 模式启动服务端，逐项验证：签名制品入库被接受、未签名
被拒绝（NFR-S2）、下载完整性、一次性令牌注册与复用拒绝、许可证激活与席位门控、BOM
校验/发布、任务状态机与播放门控（`waiting_idle` → 空闲恢复 → `done`）、命令通道往返、
审计与 `/dl/` 白名单，结束后自动清理。

### 10.1 音频版构建（CGO + libFLAC + ALSA，真实播放）

上面的默认包是**纯 Go 管理版**：服务端只解码 WAV（FLAC 走占位返回错误），终端 Agent
的 ALSA 播放为占位（`SETUP` 被拒绝），适用于无音频硬件的开发/管理场景。**真实的 ZAT
音频解决方案**（服务端 libFLAC 解码 → 推流 → 终端 ALSA 出声）需要 **CGO 音频版**，只能
在 **Linux** 上构建（Windows/macOS 无 CGO 工具链，默认仍走纯 Go 交叉编译）。

构建依赖：`pkg-config libflac-dev libasound2-dev flac`（Debian/Ubuntu）。

本机单架构音频包（在 amd64 / arm64 Linux 主机上）：

```sh
sudo apt-get install -y pkg-config libflac-dev libasound2-dev flac
AUDIO=1 sh scripts/build-release.sh   # 产出 release/…-audio-<arch>/ bundle + tarball
# 或仅构建并跑音频测试（ALSA null 回环 + FLAC 往返），产物在 bin/audio/：
TEST=1 sh scripts/build-audio.sh
```

ARM 交叉音频包（在 x86_64 Linux 上，multiarch；无 ARM runner 时使用）：

```sh
sudo dpkg --add-architecture arm64 && sudo dpkg --add-architecture armhf
sudo apt-get update
sudo apt-get install -y gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf \
    libasound2-dev:arm64 libasound2-dev:armhf libflac-dev:arm64
sh scripts/build-audio-cross.sh       # 产出 bin/audio-cross/ 的 arm64/armhf 音频 deb
```

运行库依赖：音频版服务端需 `libflac8`（Ubuntu 24.04+ 为 `libflac12`，安装器会检测并
提示）；音频版终端 deb 的 `Depends` 已含 `libasound2 | libasound2t64`，`dpkg -i` +
`apt-get -f install` 会自动补齐。

CI（`.github/workflows/ci.yml` 的 `audio` 任务）在 **amd64 与 arm64 原生 runner** 上编译
CGO 音频后端并执行真实音频路径验证：ALSA `null` 回环（无需声卡）+ FLAC 解码往返（断言
PCM 逐比特一致）；打 tag 时把音频 bundle 与音频 deb 追加到 GitHub Release。armhf 音频
包由 `build-audio-cross.sh` 在备好的主机上产出（GitHub 无 armhf 原生 runner）。

> 音频版与纯 Go 版的引擎名、配置/数据目录、systemd 单元完全一致，可互相替换，区别仅
> 在于是否链接 libFLAC/ALSA。部署真实播放请使用音频版 bundle（其 `server/dist/` 内为
> 音频 deb，终端经 `/dl/` 自助安装即获得播放能力）。

### 10.2 安卓终端构建（NDK c-shared + AAudio + Gradle）

安卓终端 APK 只能在 **Linux** 上构建：用 Android NDK 的 clang 把 Go 核心交叉编译成
c-shared 库（`libzatagent.so`，链接 AAudio），再由 Gradle 组装 Kotlin 前台服务 / WebView
外壳。Windows/macOS 无 NDK 工具链，无法本机构建（CI 的 `android` 任务负责产出）。

构建依赖：Go 1.27+、Android NDK r26（`26.3.11579264`）、Android SDK（platform 34 /
build-tools 34.0.0 / cmake 3.22.1）、JDK 17、Gradle 8.7+。

```sh
export ANDROID_HOME=/opt/android-sdk
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/26.3.11579264"
# 可选 release 签名（不设则 debug 签名，仍可安装）：
# export KEYSTORE_FILE=/path/release.keystore KEYSTORE_PASSWORD=… KEY_ALIAS=… KEY_PASSWORD=…
VERSION=0.3.0 sh scripts/build-android.sh   # 产出 release/zephyr-terminal-0.3.0.apk
```

脚本对 `arm64-v8a` 与 `armeabi-v7a` 各交叉编译一份 `libzatagent.so` 放入
`android/app/src/main/jniLibs/<abi>/`，CMake 编译 JNI 桥（`libzatjni.so`）链接它，
Gradle `assembleRelease` 打包 APK。纯 Go 核心单元测试（无需 NDK）：

```sh
cd agent && CGO_ENABLED=0 go test ./internal/androidcore/...
```

CI（`.github/workflows/ci.yml`）：`android` 任务在 ubuntu runner 上 provision SDK/NDK/
Gradle，构建 APK 并用 NDK `llvm-readelf` 校验两个 ABI 的 `libzatjni.so` 均按 basename
依赖 `libzatagent.so`、APK 内 native 库齐全；`android-aaudio` 任务在 API 34 x86_64 模拟器
上交叉编译并运行 player 测试，验证 AAudio `open/write/drain/close`（S16/S24/S32）真实
出声路径。打 tag 时把 `zephyr-terminal-<version>.apk` 追加到 GitHub Release。