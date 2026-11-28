# Zephyr HiFi Kit · 版本更新记录 / Changelog

和风HiFi音乐播放套件。最新版本在前，版本号与 Git 标签、GitHub Releases 一致。
Newest first; version numbers match Git tags and GitHub Releases.

## 0.70 — 2026-11-28

- 设置维护统计提速：cover_hash 三表加索引，孤立封面判定从约 28s 降至毫秒级（此前设置页打开需等待近 10s）。
- 扫描断点续跑：_walk 游标持久化，进程重启后自动从中断处继续，长 NAS 扫描不再因部署/重启从头再来。
- 启动时自动恢复被中断的扫描（2h 上限），完成即清除游标。

## 0.69 — 2026-11-28

- 修复 24bit 音频投送噪音：流送分帧不再丢弃不足整帧的尾部字节（24bit WAV/非对齐读路径此前每 chunk 丢字节导致相位错乱）。
- 修复暂停/恢复后终端无声：ALSA start_threshold 改为一个 period，underrun prepare 后立即恢复播放。
- 封面：新增 ID3v2 APIC/PIC、MP4 covr、Ogg/Opus、APEv2 内嵌封面解析；FLAC 目录内逐首重试；扫描回填存量封面。
- 扫描改异步任务：设置·音乐文件夹显示进度条与实时计数（已处理/新增/更新/封面回填），断连不中断扫描。
- 备份与维护从设置分区独立为侧栏顶级页；维护概况新增封面/歌词缓存容量与孤立封面、专辑数统计。
- 应用目录更名组件库、注册应用更名添加组件；按参考组件表入库 8 个协议组件（角色/架构/安装策略），关于我们同步组件来源。
- 播放控制台 EQ 改开关样式胶囊（文字 EQ + 迷你开关），播放栏 EQ 图标移至音量后并统一图标格式。
- 终端管理新增重启/关机入口（管理员审计代理）；Agent 升级本地缺包时自动下载最新签名包。
- 新增歌词懒缓存（内嵌/同名字幕）与终端歌词读取接口。
- 新增 hifihub-player MCP 插件（stdio）：供 Qoder/WorkBuddy 等智能体检索曲库与控制播放。
- 补提交 0.68 遗漏的 agent discover 包源码。

## 0.68 — 2026-11-24

**中文**
- 每播放设备独立 DSP：16 段图形 EQ、11 组常用参考曲线（含流行、爵士、乡村、古典）、最多 8 组 PEQ、前置增益和软限幅；配置持久化并支持运行中热更新，MQA/DoP 位完美路径自动绕过。
- 播放状态按设备隔离：队列、随机顺序、播放模式、音量和定时关闭使用稳定的 `node_id::hw` 标识，升级时迁移旧终端状态。
- DSD 安全默认值：新设备默认 PCM 转换，仅对明确选择 DoP 的输出启用 DoP；避免 DAC 把 DoP 标记帧当作 PCM 输出噪音。
- 音频设备中心：终端列表与播放设备表格整合到设置，支持多 DAC 激活、命名、DSD 模式、EQ 状态与删除终端；播放栏和控制台只显示播放设备名。
- 管理安全与接入：本机 localhost 自动签发管理员会话，远程敏感操作仍要求登录；DSP 修改纳入管理员门禁。安卓安装令牌改为 6–8 位数字验证码，Linux 令牌保持兼容。
- 曲库整理与音频修复：补强 ID3v2/ID3v1、DSF、DFF、AIFF、Ogg、MP4 标签解析与目录兜底；整理支持预览、计划校验、事务回填且不修改音频文件。修复高位深/高采样率投送噪音与无年份专辑推送按钮。

**English**
- Per-device DSP: 16-band graphic EQ, eleven reference curves including pop, jazz, country and classical, up to eight PEQ filters, preamp and soft limiting; settings persist and update live, while MQA/DoP bit-perfect paths bypass DSP automatically.
- Device-isolated playback state: queues, shuffle order, playback mode, volume and sleep timers use stable `node_id::hw` identities, with legacy terminal state migrated on upgrade.
- Safe DSD defaults: new devices convert DSD to PCM unless DoP is explicitly selected, preventing unsupported DACs from playing DoP marker frames as noisy PCM.
- Audio Devices hub: terminals and playback-device tables now live in Settings with multi-DAC activation, naming, DSD mode, EQ status and terminal deletion; the player bar and console show playback-device names only.
- Administration and enrollment: localhost receives a trusted admin session automatically while remote sensitive operations still require login; DSP writes are admin-gated. Android enrollment uses 6–8 digit numeric codes, with Linux token compatibility retained.
- Library and audio fixes: stronger ID3v2/ID3v1, DSF, DFF, AIFF, Ogg and MP4 tag parsing plus directory fallbacks; organization supports previews, plan validation and transactional backfill without modifying audio files. Fixed high-bit-depth/high-rate streaming noise and push buttons for albums without year metadata.

## 0.67 — 2026-11-23

**中文**
- 播放设备激活制：终端列表行内新增默认展开的「播放设备」条，列出终端上报的全部输出；管理员可激活并自定义命名（如「客厅 DAC」），登记持久化在服务端；升级时自动把当前偏好设备迁移为已激活，播放不中断。
- 播放控制台设备选择改造：播放器弹层只列已激活的播放设备（自定义名优先、未上报禁用）；移除原始 ALSA 输出选择与配置弹窗的「DAC 仲裁 / DAC 设备」控件，仲裁保留服务端自动运行。
- 服务端边界：SetDevice 拒绝未激活设备；设备激活与命名接口管理员门禁并入审计。

**English**
- Activated playback devices: each terminal list row gains an always-expanded inline device strip listing every reported output; admins activate and custom-name them (e.g. "Living DAC"), persisted server-side; upgrades migrate the current preferred device as activated so playback never breaks.
- Player device picker rework: the player popover lists only activated playback devices (custom names first, absent ones disabled); the raw ALSA output picker and the config modal's DAC arbitration/device controls are removed, arbitration keeps running automatically server-side.
- Server boundary: SetDevice rejects unactivated devices; activation and naming endpoints are admin-gated and audited.

## 0.66 — 2026-11-23

**中文**
- 设备热插拔同步：ZAT STATUS 新增可选设备列表字段；终端以 /proc/asound/cards 探测声卡变化并随心跳上报，DAC 插拔或开关机后控制台设备列表秒级刷新，无需重连；旧终端不受影响（需终端 Agent 0.66）。
- 设备名称展示：终端抽屉「设备与维护」的 ALSA 输出选项与 DAC 设备行显示终端上报名称（如 AUDIO · hw:3,0），不再只有 hw 编号。
- 设置页排版：修复音乐文件夹 / 备份与维护 / 关于我们各卡片黏连无间距的问题，分区内卡片间距统一 16px。

**English**
- Hotplug sync: ZAT STATUS carries optional ALSA device-list fields; the agent probes /proc/asound/cards for card changes and reports them with its heartbeat, so the console device picker refreshes within seconds of a DAC plug/unplug or power cycle without reconnecting. Older agents are unaffected (requires terminal agent 0.66).
- Device names: the terminal drawer's Devices & Maintenance tab shows reported ALSA names in the output picker and the DAC device row (e.g. AUDIO · hw:3,0) instead of bare hw ids.
- Settings spacing: fixed cards sticking together in Music Folders / Backup & Maintenance / About; card spacing within each section is now a uniform 16px.

## 0.65 — 2026-11-23

**中文**
- 专辑封面效率：封面按专辑目录一次性解析（内嵌优先，旁置 cover/folder 等图片兜底），惰性生成 96/256/384/640 四档缩略索引并按显示尺寸供给，immutable 缓存；列表不再整张下发原图，清晰度与流量兼顾。
- 专辑详情页：独立页面展示大封面与艺术家/年份/风格等信息；曲目支持多选与批量入队/推送；相似专辑独立成行（无相似时隐藏）；新增「打开文件夹」定位到源目录；窄屏封面与信息居中。
- 界面样式统一：清理死代码并加入样式守卫测试；弹层样式合并、颜色收敛为 RGB 变量、静态内联样式迁入类；各页页头、工具栏、空状态与表单统一为共享组件。
- 安全加固：告警确认/全部确认/解决/扫描纳入管理员门禁；ZAT HELLO 的密钥摘要比对改为常量时间比较；控制台所有响应增加 nosniff 与禁止 iframe 嵌套头；govulncheck 对服务端与终端模块扫描均无已知漏洞。

**English**
- Cover efficiency: covers resolve once per album directory (embedded first, sidecar cover/folder images as fallback) and lazily render 96/256/384/640 px indexed variants served at display size with immutable caching; lists no longer download full originals.
- Album page: a dedicated page with a large cover and artist/year/genre info, multi-select tracks with bulk queue/push actions, a separate related-albums row (hidden when empty), an "open folder" jump to the source directory, and a centered hero on narrow screens.
- Style unification: dead CSS removed behind a style-guard test; popovers merged, colors consolidated into RGB tokens and static inline styles moved into classes; page headers, toolbars, empty states and forms share common components.
- Security hardening: alert ack/ack-all/resolve/scan now require the admin session; the ZAT HELLO secret-digest comparison is constant-time; every console response carries nosniff and frame-ancestors 'none'; govulncheck reports no known vulnerabilities for the server and agent modules.

## 0.64 — 2026-11-22

**中文**
- 播放队列：终端空闲时「加入队列」「下一首播放」「替换队列」立即起播（队列非空也生效），暂停中的会话不被顶掉；播放中入队仍不打断。
- 推送稳定性：MQA 检测加入 20 秒超时保护与阶段日志，网络挂载读取卡住时不再无限阻塞推流；检测并发限流并后台回写结果。
- 终端接入：添加终端新增「SSH 自动安装」，一次填写主机/账号/密码或私钥，服务端探测架构与系统、展示并校验主机指纹后自动安装，凭据不落盘，最多两个并发任务。
- 应用目录：新增「获取方式」分类（发行版源 / 厂商公开包 / 本地上传），列表分为「公开应用」与「自备上传」两组，新建时可选、管理员可随时重分类。
- 播放栏与控制台：三区布局重排，终端与输出设备改为图标弹层，音量改为图标+滑杆弹层，收藏靠右，播放控制+进度同组；窄屏自动两行堆叠。
- 定时关闭：15/30/60/120 分钟后停止播放，按终端持久化，服务端到点自动停止，播放栏与控制台显示倒计时。
- 交互修复：弹窗内下拉弹层无法点击（背景层拦截默认行为）与定位越界（改用原生 Popover 顶层渲染）问题；音量弹层不再压住进度条。
- 设置：授权信息精简（移除无效的信任根操作、许可证扩容折叠为高级项、密钥卡改为验签公钥），BOM 部署模板折叠为高级项，自升级卡按服务可用性显示。

**English**
- Queue: add-to-queue, play-next and replace-queue start playback immediately on an idle terminal even when the queue is non-empty; paused sessions are never displaced, and queue edits never interrupt playback.
- Push reliability: MQA detection is bounded to 20 seconds with stage logging, so stalled network-mount reads can no longer block streaming; detection is concurrency-limited with background result persistence.
- Onboarding: the add-terminal wizard gains SSH auto-install. Enter host, account and password or private key once; the server detects arch/OS, shows and verifies the host fingerprint, then installs. Credentials are never stored and at most two jobs run concurrently.
- App catalog: new "obtained via" classification (distro / vendor / local upload); the list splits into Public apps and Self-supplied groups, selectable on creation and reclassifiable by admins.
- Player bar and console: three-zone layout with terminal/device icon popovers, an icon + slider volume popover, favorite at the far right, transport and seek grouped; narrow widths stack into two rows.
- Sleep timer: stop playback after 15/30/60/120 minutes, persisted per terminal and enforced server-side, with a countdown in the bar and console.
- Interaction fixes: popover menus inside modals could not be clicked (modal backdrop swallowed the default action) and were positioned off-screen; they now render in the native top layer. The volume popover no longer overlaps the seek bar.
- Settings: authorization simplified (dead trust-root actions removed, license expansion folded into Advanced, keys card renamed to verification keys), BOM templates folded into Advanced, and the self-update card follows service availability.

## 0.63 — 2026-11-22

**中文**
- CUE：同目录 `.cue` 解析为独立曲目（WAV/FLAC 位完美样本切片），被引用的整轨隐藏；播放时长、进度与 seek 均按切片区间计算。
- 标签修复：对写入为“?”的标签按文件名/上级目录回退修复，支持预览与一次性存量回填；无法恢复的艺术家显示为“未知艺术家”。
- 专辑：卡片显示封面、艺术家、年份、风格与收藏心形，艺术家可点击；相似专辑只在专辑详情显示，列表页不再展示。
- 音乐库：曲目统一为列表视图；艺术家/专辑/曲目保留服务端分页与跳页；侧栏“音乐库”“播放列表”二级菜单默认展开。
- 播放列表：新增独立详情页（重命名/删除/推送、↑↓排序、单曲移除、添加曲目指引），侧栏直接列出各播放列表。
- 播放控制台：搜索框语音输入（Web Speech），新增全套播放快捷键与 `?` 帮助浮层；修复 MQA/DSD 徽章错位。
- 导航与设置：操作日志改为右侧抽屉（加宽并完整显示）；设置合并为单页（顶部锚点、滚动联动）；修正导航顺序、高亮联动与溢出遮挡。
- 收藏：专辑批量收藏状态与心形即时切换，新增批量收藏键接口。

**English**
- CUE: cue sheets expand into virtual sample-window tracks (bit-perfect WAV/FLAC), the referenced image track is hidden, and duration/progress/seek use the window clock.
- Tag repair: lossy "?" tags are rebuilt from file names and parent folders, with preview and a one-time stock backfill; unrecoverable artists fall back to Unknown artist.
- Albums: cards show cover, artist, year, genre and a favorite heart with clickable artists; related albums appear on detail views only.
- Library: one list layout for tracks, retained server paging/jump; the sidebar Library and Playlists groups start expanded.
- Playlists: dedicated detail pages with rename/delete/push, reorder, per-track removal and add-track guidance.
- Console: voice search, full playback hotkeys with a help overlay, and aligned MQA/DSD badges.
- Navigation/settings: audit moved to a wider right drawer; settings merged into one anchored page with scroll sync; fixed nav order, highlighting and overflow.
- Favorites: batch album favorite state with instant hearts and a batch keys endpoint.

## 0.62 — 2026-11-21

**中文**
- 修复：仅接受 32 位 ALSA 槽位的直连 DAC（如 AUDIO）无法播放 16-bit MQA-CD；严格直通现将 16 位样本零损失左对齐装入 32 位 hw。
- 启动时自动重连状态为“已挂载”的音乐源（支持 NAS 启动较晚的重试），避免重启后 SMB 会话丢失。
- DSD→PCM 回退可使用已安装的 ffmpeg；服务端通过 `HIFIHUB_FFMPEG` 指定可执行文件。
- 播放控制：模式选择改为收藏旁的模式图标，点击展开顺序/列表循环/单曲循环/随机四种线性图标；MQA 仅在检测到 MQA 音源时显示金色图标。
- 播放模式权限：按终端切换播放模式不再要求管理员登录，底栏快捷图标与弹出控制台内的模式图标均可直接切换。
- 艺术家索引：扫描时物化艺术家索引（排序键、首字母、曲目数），前端一次缓存并按 A–Z/0–9/# 平铺瓦片筛选，艺术家检索从数百毫秒降至毫秒级；艺术家以首字图标墙展示。
- 分页：暂时固定每页数量（艺术家 27、专辑 28、曲目 24），移除自适应档位；新增首页/末页、上一页/下一页、页码输入跳转和当前范围显示。
- 曲目视图：默认瓦片式封面网格（标题、艺术家/专辑、时长、编码与 MQA/DSD 标记），可在工具栏切换列表并记住选择。
- 首页“新添加的专辑”改为按专辑内最晚入库曲目时间倒序，展开最多显示 50 张；分组与排序结果缓存，减少总览重绘开销。

**English**
- Fix: bit-perfect playback failed on direct DACs that only expose 32-bit ALSA slots (for example MQA-CD 16-bit on AUDIO); 16-bit samples are now zero-padded into 32-bit hw slots without losing payload bits.
- Saved music sources marked mounted are reconnected on startup, with retries while a NAS finishes booting, so a reboot no longer loses the SMB session.
- The DSD→PCM fallback can use an installed ffmpeg; `HIFIHUB_FFMPEG` points the server at the executable.
- Playback controls: the text mode selector is now a mode icon beside the favorite button, opening four linear icons (sequence, repeat all, repeat one, shuffle); the gold MQA icon appears only for detected MQA sources.
- Playback mode no longer requires an admin login; both the player-bar shortcut and the icons inside the player console switch immediately.
- Artist index: a materialized artist index (sort key, initial, track count) is built during scans and cached once by the client, with A–Z/0–9/# tiles and a monogram artist wall; artist lookup drops from hundreds of milliseconds to milliseconds.
- Pagination: per-page counts are temporarily fixed (artists 27, albums 28, tracks 24), with first/last, previous/next, page-number jump and a visible result range.
- Tracks: a tile view with cover, title, artist/album, duration and codec/MQA/DSD badges is now the default, with a remembered list-view toggle.
- Home "Newly added albums" now sorts by the album's latest-added track (newest first) and expands to at most 50; grouping and ordering are cached across overview repaints.

## 0.61 — 2026-11-21

**中文**
- DSD 透传：服务端解析 DSF/DFF，按 DoP（DSD over PCM）打包 24 位帧（0x05/0xFA 标记逐帧交替），ZAT 以 176.4/352.8kHz 等载波速率推流；终端只用严格 `hw:` 直通并锁定软件音量。
- DSD 回退：终端不接受 DoP 时自动改用 ffmpeg 解码为 PCM（标记为“DSD→PCM 回退”），保证可播放；未安装 ffmpeg 时给出明确错误。
- 服务端主动配置：终端离线时，服务端先以节点密钥 HMAC 校验终端身份，再向其本地接口下发当前服务端地址，终端持久化并自动重连；终端侧扫描作为兜底。无需手动修改终端配置。
- 音乐库与播放栏显示 DSD/DoP 状态；DSD 曲目首次播放后会补齐时长与规格。

**English**
- DSD passthrough: the server parses DSF/DFF and packs 24-bit DoP frames (0x05/0xFA markers alternating per frame) over ZAT at 176.4/352.8 kHz carriers; terminals use a strict `hw:` path with software volume locked.
- Fallback: when the terminal cannot take DoP, the server decodes with ffmpeg to PCM and marks the session as converted; a clear error is returned when ffmpeg is absent.
- Server-driven configuration: for an offline terminal the server first verifies the terminal with a node-secret HMAC, then pushes its current address to the terminal's local API; the terminal persists it and reconnects. Client-side scanning remains a fallback; no manual terminal configuration is needed.
- DSD/DoP state is shown in the library and player bar; first play fills in DSD duration and format metadata.

## 0.60 — 2026-11-20

**中文**
- MQA（含 MQA-CD）：扫描识别标签，并在曲库扫描/首次播放时解析前 3 秒 PCM 的 36 位同步字，无标签文件也能识别。
- 位完美直通：MQA 曲目只允许 `hw:` 直连设备，禁用插件回退、重采样与软件音量（锁定 100%），24 位音频可零损失装入 32 位 hw 槽位。
- 终端与界面：ZAT 协商新增 MQA 能力与 `bit_perfect` 回执；音乐库、队列、播放栏显示 MQA 标记并锁定音量。MQA 展开仍由 DAC 完成，不做软件 unfolding。
- 不满足条件的终端或输出设备会拒绝播放并给出明确原因，避免无声降级。

**English**
- MQA / MQA-CD detection from tags and, for untagged files, the 36-bit sync word in the first three seconds of PCM, both at scan time and on first play.
- Bit-perfect passthrough: MQA tracks only play on direct `hw:` devices with no plug fallback, resampling or software volume (locked at 100%); 24-bit payloads may use zero-padded 32-bit hw slots.
- ZAT exposes MQA capability and a `bit_perfect` acknowledgement; library, queue and player bar show MQA and lock the volume. Unfolding stays in the DAC.
- Endpoints or outputs that cannot guarantee the path refuse playback with an explicit reason instead of degrading silently.

## 0.59 — 2026-11-20

**中文**
- 音乐库：曲目支持逐项勾选，新增批量操作栏，可加入队列、下一首播放、替换队列、加入播放列表，并支持选择当前页或全部筛选结果。
- 播放模式：按终端保存顺序 / 列表循环 / 单曲循环 / 固定随机顺序，跨浏览器与终端一致，自动续播遵循该模式。
- 播放栏：点击控制栏空白区域可打开播放控制台。
- 总览：最近听过拆分为“歌曲 / 专辑”两个标签页，新增基于播放历史的最近歌曲。

**English**
- Library: per-track checkboxes with a bulk bar for enqueue, play next, replace queue, add to playlist, plus select-page and select-all-filtered.
- Playback modes per endpoint: sequence / loop-all / loop-one / frozen shuffle, persisted server-side and honored by auto-advance.
- Player bar: clicking the bar (outside controls) opens the player console.
- Overview: "Recently played" now has Songs / Albums tabs with track-level history.

## 0.58 — 2026-11-20

**中文**
- 新增：Windows / 非 CGO 构建接入纯 Go FLAC 解码（16/24/32-bit），不依赖 libFLAC 或外部程序，ZAT 仍推送 PCM。
- 依赖：vendor 公共领域 FLAC/bitio/errutil 代码，离线可构建。
- 说明：MQA 与 DSD 支持（P2/P3）尚未包含；MQA 仍只能输出 core PCM，DSD 透传待后续版本。

**English**
- The Windows / non-CGO server now decodes FLAC (16/24/32-bit) in pure Go and keeps streaming PCM over ZAT.
- Public-domain FLAC/bitio/errutil sources are vendored for offline builds.
- MQA unfolding and DSD passthrough remain for P2/P3.

## 0.57 — 2026-11-20

**中文**
- 修复：音乐库专辑列表使用的 `img.cover.lb-art` 仍会裁切图片；现与专辑网格一致改为完整显示。
- 说明：0.57 是 0.56 的封面显示修复版，其余 0.56 内容不变。

**English**
- Fix: album-list covers (`img.cover.lb-art`) still cropped; they now display in full like the album grid.
- 0.57 is a cover-display bugfix on top of 0.56.

## 0.56 — 2026-11-20

**中文**
- 封面：专辑图改为完整显示，不再裁切；支持 `folder.jpg`/`cover.png`/`front.webp` 等侧车图片（同一目录及多碟布局的父目录，取最大有效图）。
- 标签编码：Windows 服务端可自动识别 GBK、Big5、Shift-JIS、Windows-1251 标签，也允许在“编辑源”中手动指定编码；合法 UTF-8 不会被转换。
- 说明：编码设置保存在音乐源上；修改后需执行一次“全量重扫”以更新已索引的元数据与封面。
- 限制：非 Windows 服务端暂不转码旧编码；MP3/AAC/DSD 标签解析仍未加入（本次仅 FLAC/WAV）。

**English**
- Covers: album art is shown in full, and conventional sidecar images (`folder.jpg`, `cover.png`, `front.webp`, ...) are used when no embedded art exists.
- Text: Windows servers auto-detect GBK, Big5, Shift-JIS and Windows-1251 tags, with a per-source manual override; valid UTF-8 is never converted.
- Run a full rescan after changing the encoding. Legacy decoding currently applies to FLAC/WAV tags on Windows only.

## 0.55 — 2026-11-20

**中文**
- 修复：部分终端输出设备只接受特定 PCM 格式（例如 `AUDIO` 仅接受 S32），`hw:X,Y` 打开失败时自动回退到 `plughw:X,Y`，由 ALSA 完成格式转换。
- 行为：严格 `hw:` 设备仍优先；只有打开或参数设置失败时才使用 `plughw:`。回退路径可能进行格式转换，不再保证设备原生位完美。
- 说明：该修复只影响终端 PCM 输出格式；Windows 管理版服务端仍不能解码 FLAC。

**English**
- Fix: when a terminal device rejects the negotiated PCM format, fall back from `hw:X,Y` to `plughw:X,Y` so ALSA can convert.
- The strict hardware path remains preferred; the fallback may convert formats and is not guaranteed bit-perfect.

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
