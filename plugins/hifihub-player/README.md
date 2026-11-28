# hifihub-player

让智能体（Qoder、WorkBuddy 等任意 MCP 客户端）检索 Zephyr HiFi Kit 曲库并控制终端播放的插件。

## 组件

- `skills/hifihub-player/SKILL.md`：播放控制工作流与安全边界。
- `.mcp.json` + `bin/hifihub-mcp[.exe]`：stdio MCP server（Go 实现，源码在服务端仓库 `cmd/hifihub-mcp`），把 21 个播放类工具代理到服务端 admin HTTP API。仓库内不含可执行文件，二进制由 CI 随 Release 分发（见下）。
- 不含终端重启/关机、音乐源删除、管理员设置等高危能力。

## 工具一览

| 工具 | 作用 |
| --- | --- |
| list_nodes / list_playback_devices / list_devices | 终端、可播放设备、ALSA 设备发现 |
| player_status | 播放状态（曲目/pts/缓冲/音量/设备） |
| play_track / pause / resume / stop / seek / set_volume | 播放控制 |
| select_device | 切换播放设备 |
| queue_get / queue_set / queue_enqueue / queue_next / queue_prev / queue_jump / queue_mode | 队列管理 |
| search_library | 曲库检索（albums/tracks/artists/genres/years） |
| eq_get / eq_set | 每设备 EQ/DSP 配置 |

## 配置

环境变量：

- `HIFIHUB_BASE_URL`：服务端地址，默认 `http://127.0.0.1:8080`。MCP server 与服务端同机时走 loopback 自动免密认证。
- `HIFIHUB_SESSION_COOKIE`：可选。远程部署时提供已登录会话 cookie（`name=value`）；不要填管理员密码。

### Qoder

安装本插件后自动加载 `.mcp.json`（`${QODER_PLUGIN_ROOT}` 指向插件目录）。

### WorkBuddy / 其他 MCP 客户端

在客户端的 MCP 配置中加入（路径换成本插件实际位置）：

```json
{
  "mcpServers": {
    "hifihub-player": {
      "command": "D:/path/to/hifihub-player/bin/hifihub-mcp.exe",
      "env": { "HIFIHUB_BASE_URL": "http://127.0.0.1:8080" }
    }
  }
}
```

## 安装

每次服务端发布都在同一个 GitHub Release 里附带三个平台安装包（本插件自身仍走 0.1.x 独立版本，安装包对应的服务端版本为 `@ZEPHYR_PRODUCT_VERSION@`，发布时由 `scripts/build-plugin.sh` 写入）：

| 资产 | 平台 |
| --- | --- |
| `Zephyr-HiFi-Kit-mcp-plugin-<版本>-windows-amd64.zip` | Windows 10/11 x64 |
| `Zephyr-HiFi-Kit-mcp-plugin-<版本>-linux-amd64.zip` | Linux x86_64 |
| `Zephyr-HiFi-Kit-mcp-plugin-<版本>-linux-arm64.zip` | Linux arm64（树莓派 4/5、NAS ARM） |

解压后把 `hifihub-player/` 目录放进 Qoder 插件目录即可；包内 `.mcp.json` 已按平台写好可执行文件名，`SHA256SUMS` 可校验完整性。其他 MCP 客户端只需把 `command` 指向包内 `bin/hifihub-mcp`。

### 从源码构建

服务端仓库根目录执行（产出上表的三个 zip）：

```sh
sh scripts/build-plugin.sh
```

只想手出一个二进制时：

```sh
go build -o bin/hifihub-mcp ./cmd/hifihub-mcp          # linux/macOS
GOOS=windows GOARCH=amd64 go build -o bin/hifihub-mcp.exe ./cmd/hifihub-mcp
```

## 验证

隔离环境端到端脚本见服务端仓库 `.tmp-ui-build/mcp-check.cjs`（fixture 服务端 + stdio JSON-RPC 全工具链路）。