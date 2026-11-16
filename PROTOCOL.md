# Zephyr Audio Transport (ZAT) 协议规格 · v0.1

> 状态：功能验证阶段草案（vertical slice）。本协议是 Zephyr Music Player 自有的
> 音频传输协议，设计参照 Roon RAAT 的「服务端解码 → 缓冲流式推送 → 终端时钟驱动播放」
> 模型，但为独立实现，不兼容也不复用 RAAT 的私有报文。
>
> 本文件位于仓库根目录，**不属于官网 `docs/`**，不会被 GitHub Pages 发布。

---

## 1. 目标与范围

ZAT 把「真正的音频解决方案」融入现有的 Zephyr 机群下发/监控平台：

- 服务端从 **SMB/CIFS 挂载** 的媒体库取音频，**识别格式**，**解码为 PCM**，
  经 ZAT **流式推送** 到指定终端。
- 终端 Agent 接收 PCM，经 **ALSA** 落地播放，并把播放状态回灌到现有的
  心跳 / 播放门控 / 审计体系。

ZAT 只负责「源 → 终端」的音频传输与播放控制；机群编排、签名下发、灰度、
席位授权、审计仍由现有平台承担，ZAT 复用其鉴权与节点模型。

### v0.1 垂直切片（本阶段交付）

| 维度 | v0.1 范围 |
| --- | --- |
| 编解码 | FLAC（CGO libFLAC 解码）、WAV/PCM（直读）。其余格式先**识别入库**，解码留 v0.2 |
| 传输 | TCP，长度前缀帧；控制 payload 用 JSON，音频 payload 用二进制 PCM |
| 终端数 | 单终端单会话（多房间同步留 v0.2） |
| 输出 | ALSA `hw:` 真实设备 + `plug:null` 回环（CI 无硬件验证） |
| 采样 | 源码原生采样率/位深直出，不做重采样（重采样留 v0.2） |
| 来源 | 内核 CIFS 挂载（`mount.cifs`）的媒体库目录 |
| 鉴权 | 复用每节点 secret（注册时签发），可选 TLS |

### v0.2+ 路线图（非本阶段）

UDP/QUIC 低延迟传输与自研可靠层、多房间时钟同步、MP3/ALAC/AAC/DSD 解码
（ffmpeg/minimp3）、重采样（libsamplerate）、音量归一化、与 AirPlay2/RAAT
终端的互通网关。

---

## 2. 角色与拓扑

```
        ┌────────────────────────── Server (x86_64 Linux) ──────────────────────────┐
        │  SMB/CIFS 挂载 ── 媒体库扫描/格式识别 ── 解码(libFLAC) ── ZAT 源端          │
        │  节点注册表(NodeID/IP/secret) ── 心跳/播放门控/审计 ── 控制台 UI            │
        └───────────────▲───────────────────────────────────────────┬───────────────┘
                        │ 控制+音频 (TCP, 终端主动外连)               │ 现有 HTTP/JSON
                        │                                            │ (注册/心跳/拉任务)
        ┌───────────────┴────────────────────────────────────────────▼──────────────┐
        │  Endpoint / 终端 Agent (amd64/arm64/armhf)                                  │
        │  ZAT sink ── 预缓冲 ── ALSA 播放 ── 本地播放 UI ── playing/active_proto 上报 │
        └────────────────────────────────────────────────────────────────────────────┘
```

**连接方向：终端主动外连服务端**（与现有「Agent 拉取、服务端是唯一下发源」模型一致，
NAT 友好）。服务端不主动拨号终端；服务端对终端本地 UI 的访问仍走现有反向代理
（`internal/api/proxy.go` 注入 `X-Node-Secret`）。

- **源端（Source）**：服务端 ZAT 监听端口（默认 `tcp/7800`，可配置）。
- **汇端（Sink）**：终端 Agent 内的 ZAT 客户端。

---

## 3. 与现有平台的集成点（复用，不重造）

| 现有机制 | 文件 | ZAT 如何复用 |
| --- | --- | --- |
| 节点注册 + 每节点 secret | `internal/node/node.go` `Register` | ZAT HELLO 用 `node_id` + secret 鉴权；secret 永不落日志 |
| 心跳播放状态 | `Heartbeat.Playing/ActiveProto/DacHolderPID` | ZAT 播放中 `Playing=true`、`ActiveProto="zat"`、`DacHolderPID=`ALSA 持有进程 |
| 服务端下发策略 | `HeartbeatResult.DacDevice/DacArbitration/PlayProtect` | ZAT 用 `DacDevice` 选 ALSA 设备；遵守 `DacArbitration`；`PlayProtect` 影响门控 |
| 播放门控 FR-3.10 | 服务端 + 终端双侧 | ZAT 播放即「正在播放」，系统升级/停服务/关机被既有门控拦截（409） |
| 本地控制门禁 | `ALLOW_LOCAL` + `X-Node-Secret` | 终端本地播放 UI 的变更路由沿用同一门禁 |
| 审计 | 现有审计 + CSV/JSON 导出 | ZAT 会话建立/播放/停止/错误写审计事件 |
| 发现协议诊断 | mDNS/SSDP | ZAT 服务以 `_zephyr-audio._tcp` 广告，复用现有发现/诊断栈 |

---

## 4. 发现与会话建立

1. 服务端通过 mDNS 广告 `_zephyr-audio._tcp.local`，TXT 记录含 `port`、`ver=1`、
   `caps=flac,wav`、`node=<server-id>`。终端亦可由控制台「播放到终端」指令触发
   （指令经现有命令通道下发，含服务端 ZAT 地址与会话令牌）。
2. 终端拨号服务端 ZAT 端口，发送 `HELLO`（携带 `node_id`、secret 摘要、能力）。
3. 服务端校验 secret 与节点状态（在线、已授权、席位充足），回 `HELLO_ACK`
   （携带会话 ID、服务端能力、 negotiated 默认参数）。
4. 服务端发 `SETUP`（曲目、PCM 格式、采样率、声道、预缓冲毫秒、ALSA 设备建议）。
5. 终端回 `SETUP_ACK`（确认 ALSA hw params 可开），进入 `READY`。
6. 服务端发 `PLAY` 并开始推送 `AUDIO` 帧；终端预缓冲达标后开始 ALSA 写入。

> secret 鉴权：HELLO 携带 `node_id` 与 `secret_sha256`（secret 的 SHA-256，
> 避免明文传输；启用 TLS 时可直接带 secret）。服务端按节点注册表比对。

---

## 5. 传输层与帧格式

v0.1 使用**单条 TCP 连接**复用控制与音频（控制帧小且少，音频帧大且连续；
TCP 保证有序可靠，简化垂直切片）。v0.2 再拆分控制/音频通道或改 UDP/QUIC。

### 5.1 帧头（固定 16 字节，大端）

```
偏移  长度  字段          说明
0     4     Magic         ASCII "ZAT1" = 0x5A 0x41 0x54 0x31
4     1     Version       0x01
5     1     Type          消息类型（见 5.2）
6     2     Flags         bit0=音频帧 bit1=TLS 内 bit2=需 ACK（其余保留 0）
8     4     Seq           uint32，每连接单调递增（控制与音频共用计数器）
12    4     PayloadLen    uint32，payload 字节数（不含帧头）
16    ...   Payload       控制帧=JSON(UTF-8)；音频帧=二进制（见 5.3）
```

- 接收方先读 16 字节帧头，校验 Magic/Version，再按 `PayloadLen` 读 payload。
- `PayloadLen` 上限 v0.1 为 1 MiB（音频帧典型 ≤ 64 KiB）；超限即 `ERROR` 并断连。
- 帧解析为纯 Go，可无 CGO 单测（见任务 #70）。

### 5.2 消息类型

| Type | 名称 | 方向 | Payload | 说明 |
| --- | --- | --- | --- | --- |
| 0x01 | HELLO | sink→source | JSON | 鉴权 + 能力声明 |
| 0x02 | HELLO_ACK | source→sink | JSON | 会话 ID + 服务端能力 |
| 0x03 | SETUP | source→sink | JSON | 曲目 + PCM 格式 + 缓冲参数 |
| 0x04 | SETUP_ACK | sink→source | JSON | ALSA hw params 确认 |
| 0x05 | PLAY | source→sink | JSON | 开始/恢复播放（可带起始 PTS） |
| 0x06 | PAUSE | source→sink | JSON | 暂停（保持 ALSA 句柄） |
| 0x07 | SEEK | source→sink | JSON | 跳转到样本位置 |
| 0x08 | STOP | source→sink | JSON | 停止并 drain |
| 0x09 | VOLUME | source→sink | JSON | 音量 0..100（软音量，v0.1） |
| 0x0A | STATUS | sink→source | JSON | 周期上报：位置/缓冲/时钟/状态 |
| 0x0B | TEARDOWN | 双向 | JSON | 关闭会话 |
| 0x0C | ERROR | 双向 | JSON | 错误码 + 描述 |
| 0x0D | PING | 双向 | JSON | 保活（带 nonce） |
| 0x0E | PONG | 双向 | JSON | 保活响应 |
| 0x10 | AUDIO | source→sink | 二进制 | PCM 帧（见 5.3） |
| 0x11 | TRACK_META | source→sink | JSON | 正在播放元数据（标题/艺术家/专辑/时长/封面 URL） |

### 5.3 音频帧 payload（二进制，大端）

```
偏移  长度  字段
0     8     PTS        uint64，本帧首样本的「曲目内样本位置」（采样时钟）
8     4     TrackIDLen uint32
12    N     TrackID    UTF-8 曲目标识
12+N  ...   PCM        交错 little-endian 样本，格式由 SETUP 协商
```

- PCM 格式：`S16_LE` / `S24_LE`（3 字节紧凑）/ `S32_LE`，声道交错。
- 每帧样本数 = `len(PCM) / (channels * bytesPerSample)`。
- PTS 以**样本数**计（与采样率无关），终端按本地 ALSA 时钟消费，避免漂移。

---

## 6. 控制消息 payload（JSON 示例）

```jsonc
// HELLO (sink→source)
{ "node_id": "n-abc", "secret_sha256": "<hex>", "agent_version": "0.4.0",
  "caps": { "codecs": ["pcm"], "formats": ["S16_LE","S24_LE","S32_LE"],
            "rates": [44100,48000,88200,96000,176400,192000],
            "max_channels": 2, "alsa_devices": ["hw:0,0","hw:1,0"] } }

// HELLO_ACK (source→sink)
{ "session_id": "s-123", "server_version": "0.4.0",
  "caps": { "codecs": ["flac","wav"] }, "prebuffer_ms": 250 }

// SETUP (source→sink)
{ "session_id": "s-123", "track_id": "t-1", "format": "S24_LE",
  "rate": 96000, "channels": 2, "prebuffer_ms": 250,
  "alsa_device": "hw:1,0", "total_samples": 12345678, "gain_db": 0 }

// SETUP_ACK (sink→source)
{ "session_id": "s-123", "ok": true, "opened": { "format":"S24_LE","rate":96000,"channels":2 },
  "buffer_frames": 4096 }

// PLAY / PAUSE / STOP / SEEK / VOLUME
{ "session_id": "s-123", "start_pts": 0 }          // PLAY
{ "session_id": "s-123" }                          // PAUSE / STOP
{ "session_id": "s-123", "pts": 4800000 }          // SEEK
{ "session_id": "s-123", "volume": 80 }            // VOLUME

// STATUS (sink→source, 每 ~500ms)
{ "session_id": "s-123", "state": "playing", "pts": 4800000,
  "buffer_ms": 240, "underruns": 0, "alsa_device": "hw:1,0",
  "clock_ppm": 0, "dac_holder_pid": 4242 }

// TRACK_META (source→sink)
{ "track_id":"t-1","title":"…","artist":"…","album":"…",
  "duration_ms":210000,"codec":"flac","rate":96000,"channels":2,"cover_url":"…" }

// ERROR
{ "session_id":"s-123","code":"decode_failed","detail":"…","fatal":true }

// TEARDOWN
{ "session_id":"s-123","reason":"user_stop" }
```

---

## 7. 会话状态机

```
IDLE ──HELLO──> AUTHENTICATED ──SETUP──> READY ──PLAY──> PLAYING
                                                   PLAYING ──PAUSE──> PAUSED
                                                   PAUSED  ──PLAY───> PLAYING
                                                   PLAYING ──SEEK───> PLAYING(新 PTS)
                                                   any ──STOP──> STOPPED ──TEARDOWN──> IDLE
                                                   any ──ERROR(fatal)/断连──> IDLE
```

- 终端在 `PLAYING` 时把 `Playing=true`、`ActiveProto="zat"` 写入心跳。
- `PAUSED`/`STOPPED` 时 `Playing=false`；ALSA 句柄在 `PAUSED` 保持，`STOPPED` drain 后关闭。
- 会话超时（无 PING/PONG 且无 STATUS > 15s）视为断连，服务端停流并写审计。

---

## 8. 缓冲、时钟与播放

- **预缓冲**：终端收到 `PLAY` 后先累积 `prebuffer_ms`（默认 250ms）PCM 再开 ALSA 写入，
  降低 underrun。缓冲电平在 `STATUS.buffer_ms` 上报。
- ** pacing**：服务端按曲目采样率 pacing 推送（不一次性灌满），维持终端缓冲在
  `[prebuffer_ms, prebuffer_ms*3]` 区间；终端缓冲过高时服务端减速（v0.1 用简单水位）。
- **时钟**：PTS 以样本计；终端 ALSA 消费时钟即播放时钟。`STATUS.pts` 为当前已写入
  ALSA 的样本位置。v0.1 单终端不做跨设备同步。
- **underrun**：ALSA 返回 `-EPIPE` → `snd_pcm_prepare` 恢复，`underruns++`，
  上报 `ERROR(code=underrun, fatal=false)`；服务端可补发缺失 PTS 区间。
- **overrun**：终端缓冲超上限 → 丢弃最旧帧并上报（v0.1 简单策略）。
- **音量**：v0.1 软音量（对 PCM 样本缩放，S24/S32 注意溢出与抖动）；
  硬音量（ALSA mixer）留 v0.2。

---

## 9. 格式协商与解码

1. 终端在 `HELLO.caps` 声明 ALSA 支持的 `formats/rates/max_channels` 与设备列表。
2. 服务端按源码（FLAC STREAMINFO / WAV fmt）得到原生 `rate/channels/bits`，
   与终端能力求交集；v0.1 **不重采样**，若源码率不在终端支持列表则 `ERROR(rate_unsupported)`。
3. 位深映射：FLAC 16bit→`S16_LE`，24bit→`S24_LE`，32bit→`S32_LE`；
   WAV 按 fmt 直读。`SETUP.format` 为最终协商结果。
4. 解码：服务端 CGO libFLAC 回调输出 PCM → 打包 `AUDIO` 帧。WAV 直读 data chunk。
5. 识别（纯 Go，任务 #70）：按 magic + 头解析 codec/rate/channels/bits/duration，
   入库供浏览；识别与解码解耦，未支持解码的格式仍可识别展示。

---

## 10. Gapless 与曲目切换

- 同一会话内切曲：服务端发新 `SETUP`（新 `track_id`，格式若不变可复用 ALSA 句柄），
  终端在上一曲 PTS 到达 `total_samples` 时无缝接下一曲首帧（ALSA 不 close）。
- 格式变化需重开 ALSA：发 `STOP`→`SETUP`→`PLAY`，允许短暂间隙（v0.1）。
- 播放列表/队列由服务端控制台管理，ZAT 只承载单曲流与切换信令。

---

## 11. 安全

- **鉴权**：HELLO 用每节点 secret（SHA-256 摘要；TLS 内可明文）。secret 永不落日志、
  不入审计明文。会话 ID 绑定 node_id，跨节点串话拒绝。
- **授权门禁**：会话建立前过「许可证有效 + 终端已授权 + 席位充足」（复用现有三道检查）。
- **TLS**：v0.1 可选（`Flags.bit1` 标识 TLS 内）；生产建议启用，证书复用平台 TLS。
- **播放门控**：ZAT 播放中，系统升级/停服务/关机被 FR-3.10 双侧拦截（服务端 409，
  终端本地控制台拒绝）。ZAT 自身的 STOP/PAUSE 不受门控限制（那是播放控制本身）。
- **CIFS 凭据**：挂载凭据加密存储于服务端 DB，挂载命令经白名单 Runner 执行，
  凭据不出现在日志/审计明文（见任务 #70/#72）。

---

## 12. 错误码

| code | fatal | 含义 |
| --- | --- | --- |
| `auth_failed` | 是 | secret/节点校验失败 |
| `not_authorized` | 是 | 许可证/授权/席位不满足 |
| `rate_unsupported` | 是 | 源码采样率终端不支持（v0.1 不重采样） |
| `format_unsupported` | 是 | 位深/声道协商失败 |
| `decode_failed` | 是 | 服务端解码错误 |
| `alsa_open_failed` | 是 | 终端 ALSA 设备打开失败 |
| `underrun` | 否 | 终端缓冲耗尽，已 prepare 恢复 |
| `overrun` | 否 | 终端缓冲溢出，已丢弃旧帧 |
| `session_timeout` | 是 | 保活超时 |
| `mount_failed` | 是 | SMB/CIFS 挂载失败（服务端，会话前） |
| `internal` | 是 | 未分类错误 |

---

## 13. 一致性与验证策略

- **纯 Go 可测**（本机 Windows / 无 CGO）：帧编解码、Magic/Version/Seq 校验、
  控制消息 JSON 契约、会话状态机、格式识别 probe、CIFS 挂载命令构建。`go test ./...` 通过。
- **CGO 隔离**：libFLAC/ALSA 绑定用 `//go:build cgo && linux`；非 Linux 走纯 Go stub，
  保证全平台 `go test` 与现有 `CGO_ENABLED=0` 构建不被破坏。
- **Linux CI 回环**（ubuntu-latest + `libflac-dev`/`libasound2-dev`）：
  解码 FLAC → ZAT 流 → ALSA `plug:null` 写入，断言样本数/PTS 连续/无 underrun，
  证明闭环（无硬件）。ARM 经交叉工具链或 ARM runner 构建 deb。
- **真机出声**：在用户 Linux 终端（x86 小主机 / 树莓派）接 DAC 实测，
  本阶段不在 Windows 主机声称已出声。

---

## 14. 默认端口与配置

| 项 | 默认 | 说明 |
| --- | --- | --- |
| ZAT 监听 | `tcp/7800` | 服务端源端，可配置 |
| mDNS 服务 | `_zephyr-audio._tcp` | 发现广告 |
| 预缓冲 | 250 ms | `SETUP.prebuffer_ms` |
| STATUS 周期 | 500 ms | 终端上报 |
| 保活超时 | 15 s | 无 PING/PONG/STATUS |
| 音频帧上限 | 64 KiB PCM | 单帧 |
| PayloadLen 上限 | 1 MiB | 超限断连 |
| CIFS 版本 | `vers=3.0` | 挂载默认，可降级 2.1/3.1.1 |