# Zephyr Music Player

面向家庭与小型商用 Hi-Fi 机群的**音乐服务与终端推送**平台：一台 x86 Linux 服务端统一管理
数十至数百台播放终端（树莓派 / x86 小主机），负责签名制品仓库、BOM 期望态、灰度下发、
系统更新、网络诊断、授权席位与审计，并提供 SMB 媒体库挂载、音频格式识别与自有 ZAT 协议
（RAAT-like）推流播放；服务端自动检测新版本并自升级，终端版本跟随服务端逐环自动收敛。
管理版纯 Go（无 CGO）、SQLite 单文件存储、内嵌管理控制台；音频版经
CGO + libFLAC + ALSA 实现真实解码与比特完美出声（见 INSTALL.md §10.1）。

官方联系：services@zephyr.hk · https://www.zephyr.hk

---

## 确认功能（本仓库全部真实实现）

| 区域 | 能力 |
|---|---|
| 下发核心 | 应用目录 / 多架构制品仓库（入库强制 GPG 验签）/ BOM 草稿·校验·发布·导入·导出·回滚 / 任务状态机 + 播放门控 + 失败回滚 |
| 终端生命周期 | 一次性注册令牌（TTL 可选）/ 三步添加向导 / 终端配置（别名·房间·期望 BOM·DAC 仲裁·播放保护·维护窗口·离线阈值）/ 注销 + 撤销恢复 |
| 授权 | 离线签名许可证激活 / 席位占用 / 逐终端授权·撤销 / 下发前许可证 + 授权双门禁 |
| 运维 | 远程 apt 检查·升级（命令通道）/ mDNS·SSDP·多播网络诊断 / 内核待重启告警 / 灰度发布（环推进） |
| 自动更新 | 服务端发布源检测 + SHA256/GPG 校验自升级 + 原子置换重启 / 终端跟随服务端版本逐环收敛（并行·退避重试·挂起重试·播放保护）/ 控制台一键检查·升级·收敛·重试 |
| 安全 | enforce 验签 / 离线主密钥·签名子密钥·公钥信任根分工 / 密钥导入·信任根导出 / 终端本地控制台节点密钥门禁 / 全量审计 + CSV/JSON 导出 |
| 可观测 | 总览 KPI + 服务端消耗 / 终端资源与 DAC 占用 / 告警铃铛抽屉（确认·解决·扫描）/ 终端控制台反向代理 |
| 终端 Agent | 注册·心跳·快照 / 任务执行（apt·dpkg·配置渲染·systemd 白名单）/ DAC 仲裁执行 / 播放保护本地门禁 / 本地控制台（服务·系统更新·网络诊断·电源·正在播放） |
| 音频解决方案 | SMB/CIFS 媒体库挂载 / 音频格式识别（WAV·FLAC 等）/ 自有 ZAT 协议推流（采样时钟 PTS·节点密钥鉴权）/ 终端 ALSA 比特完美播放 / 正在播放控制台（暂停·停止·seek·音量·输出设备） |

## 已删减功能（可行性审计结论：无法落地，不做假实现）

| 删减项 | 原因 |
|---|---|
| Roon 本地包库 / 重签名 / 入库 | Roon 制品为专有签名包，第三方重签名无效且违反其分发条款；Roon Bridge 仍作为普通签名制品下发 |
| 服务端在线构建 | 需在服务端内置各应用交叉工具链，体积与攻击面不可接受；构建在离线/CI 完成，制品上传入库 |
| 二进制补丁 | 对已签名制品做二进制补丁会破坏签名链；以新版本上传替代 |
| 远程 .sh 抓取入库 | 服务端代抓公网脚本引入 SSRF 与未签名代码执行；仅保留本地上传 + 验签门禁 |
| 控制台 mock 角色切换 | 真实鉴权属于反向代理层（Nginx/Caddy + OIDC/basic-auth），控制台假定可信 LAN |

---

## 快速开始

### 服务端（x86_64 Linux + systemd）

```sh
sudo apt-get update && sudo apt-get install -y gnupg
tar -xzf zephyr-music-server-0.4.0.tar.gz
cd zephyr-music-server-0.4.0/server
sudo ./install.sh                      # 二进制 + systemd + 配置 + 信任根 + /dl 分发目录
curl -s http://127.0.0.1:8080/api/v1/health
```

浏览器打开 `http://<服务端IP>:8080/` 进入内嵌控制台。生产请在前面加 TLS 反向代理并配置鉴权。

### 终端（每台一条命令，架构自动识别）

控制台「终端管理 → 添加终端」向导签发令牌后给出同款命令；也可手动签发：

```sh
curl -s -X POST http://<服务端IP>:8080/api/v1/nodes/token \
  -H 'Content-Type: application/json' -d '{"ttl_seconds":1800}'

curl -fsSL http://<服务端IP>:8080/dl/agent-install.sh | \
  sudo bash -s -- --server http://<服务端IP>:8080 --token <一次性令牌>
```

安装器自动识别 amd64 / arm64 / armhf，从服务端 `/dl/` 拉取对应 deb，写入
`/etc/hifihub/agent.env` 并启用服务；注册成功后终端立即出现在控制台。

离线环境可用包内 deb：`sudo dpkg -i agent/hifihub-agent_0.4.0_<arch>.deb && sudo apt-get -f install -y`。

完整部署、密钥分工、许可证签发与排障见 [INSTALL.md](INSTALL.md)。

---

## 仓库结构

```
cmd/hifihub-server/      服务端入口
internal/                api / catalog / repo / bom / node / task / store / signature / config / update(自升级+收敛) / web(内嵌控制台) / media·decode·library·mount·zatserver(音频)
zat/                     自有 ZAT 音频传输协议（服务端与终端共享的独立模块）
agent/                   终端 Agent 独立模块（cmd / internal：player·zatclient / packaging / cmd/debpack）
packaging/server/        服务端 install.sh / uninstall.sh / systemd 单元 / 配置模板
scripts/                 build-release.sh / build-audio.sh / build-audio-cross.sh / smoke-test.sh
prototype/               UI 可行性验证原型（纯前端 mock，非运行代码）
```

## 构建与测试

```sh
sh scripts/build-release.sh                 # 纯 Go 管理版：交叉编译 + 三架构 deb + 发布包 + SHA256SUMS
VERSION=0.4.0 sh scripts/build-release.sh
go test ./... && (cd agent && go test ./...)
sh scripts/smoke-test.sh                    # enforce 模式端到端冒烟（需 go/gpg/curl）
```

需要 Go 1.27+；管理版 `CGO_ENABLED=0`，deb 由内置 `debpack` 生成，无需 dpkg-deb / 交叉 gcc。

音频版（Linux + CGO + libFLAC + ALSA，真实解码与比特完美出声）：

```sh
sudo apt-get install -y pkg-config libflac-dev libasound2-dev flac
TEST=1 sh scripts/build-audio.sh            # CGO 构建 + ALSA null 回环 + FLAC 往返测试
AUDIO=1 sh scripts/build-release.sh         # 本机单架构音频发布包
sh scripts/build-audio-cross.sh             # ARM 交叉音频 deb（multiarch，详见 INSTALL.md §10.1）
```

## 自动更新

服务端每 10 分钟（`HIFIHUB_UPDATE_INTERVAL`）检测发布源最新版本；开启
`HIFIHUB_UPDATE_AUTO` 后空闲时自动下载 bundle，经 feed SHA256 摘要 + 可选 GPG 验签 +
包内 SHA256SUMS 三重校验后原子置换二进制并重启，同时刷新 `/dl/` 终端分发集。终端 Agent
版本恒跟随服务端：服务端按灰度环逐批下发签名的 `agent-upgrade` 命令（下载→校验→dpkg→
重启），心跳确认版本后才算收敛；失败退避重试、达上限挂起可一键重试，播放中终端自动
等待空闲（FR-3.10）。离线 LAN 把 `HIFIHUB_UPDATE_FEED` 指向内网镜像即可。控制台
「服务下发 → 自动更新」提供全部实时状态与操作；配置与 REST 详见 INSTALL.md §11。

## 安全模型

- 生产 `HIFIHUB_SIGNATURE_MODE=enforce`：未签名制品拒绝入库；服务端只持公钥信任根。
- 主密钥离线、签名子密钥在受控签名主机、公钥为信任根（详见 INSTALL.md §4）。
- 终端 Agent 以 root 运行但动作受 systemd 单元白名单 + `ALLOW_POWER` / `ALLOW_LOCAL` 门控；
  本地控制台变更操作需节点密钥（注册时签发），服务端代理自动注入。
- 播放保护（FR-3.10）：播放中拦截下发、系统升级、停服务与关机，服务端与终端双侧执行。

## 许可

MIT · 见 [LICENSE](LICENSE)。