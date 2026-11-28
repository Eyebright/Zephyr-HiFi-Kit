---
name: hifihub-player
description: 通过 hifihub-player MCP 控制 Zephyr HiFi Kit 音乐播放。当用户要求播放/暂停/继续/停止/切歌/跳转/调音量/换播放设备/调 EQ/管理播放队列/检索曲库专辑曲目时使用。
---

# Zephyr HiFi Kit 播放器控制

使用 `hifihub-player` MCP server 的工具完成请求。所有工具返回服务端 JSON 原文；`isError=true` 时读取其中 `message` 向用户解释，不要重试同一请求超过一次。

## 前置

- MCP server 已随插件加载（stdio，`bin/hifihub-mcp.exe`）。连接失败时提示用户检查 `HIFIHUB_BASE_URL` 与服务端是否在运行，不要自行改配置。
- 本机部署自动免密认证；远程部署需要用户自行提供会话 cookie（`HIFIHUB_SESSION_COOKIE`），不得索取或代填管理员密码。

## 工作流

1. **定位目标**：先 `list_playback_devices` 获取可播放设备与 `device_key`（格式 `<node_id>::<hw>`）。多设备时若用户未指明，用 `list_nodes`/`list_devices` 展示别名让用户选择，不要猜。
2. **找曲目**：`search_library`（`category=tracks` 搜单曲、`albums` 搜专辑；专辑结果再带 `album` 过滤取曲目）。播放需要 `track_id`，一律来自检索结果，不得编造。
3. **播放**：单曲立即播用 `play_track`；整专辑/歌单用 `queue_set`（`play_if_idle=true`）；追加用 `queue_enqueue`。
4. **控制**：`pause`/`resume`/`stop`/`seek`（毫秒）/`set_volume`（0-100）/`queue_next`/`queue_prev`/`queue_jump`/`queue_mode`。操作后用 `player_status` 复核状态再回复用户。
5. **设备与 EQ**：换设备用 `select_device`（hw 必须来自 `list_devices` 且已激活）；EQ 先 `eq_get` 读现状，修改后 `eq_set` 写回完整配置对象（保留未改字段）。位完美（MQA/DoP）播放中 EQ 被旁路、音量锁定 100，遇到相关报错直接转述原因。

## 安全边界

- 只使用本插件声明的播放类工具。不得调用终端重启/关机、删除音乐源、修改管理员设置等接口。
- `stop` 会清空终端缓冲：用户只说"暂停"时用 `pause`，不要替代为 `stop`。
- 批量入队超过 50 首或整队替换正在播放的队列前，先向用户确认。
- 不读取、不输出 cookie、密码或节点 secret；工具返回中若含敏感字段不要转述。

## 回复格式

用一句话汇报结果（曲目名/设备/状态），附关键数值（位置、音量、队列长度）。失败时给出服务端错误原文与建议动作。