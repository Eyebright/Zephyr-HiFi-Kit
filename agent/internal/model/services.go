package model

// Registry is the agent's built-in service catalog. Primary services render as
// cards in the local UI; helpers are installed automatically alongside them.
// Config templates use text/template with the variables collected by ConfigFields.
var Registry = map[string]ServiceDef{
	"roonbridge": {
		AppID: "roonbridge", Name: "RoonBridge", Protocol: "raat", Category: "renderer",
		Units: []string{"roonbridge.service"}, OccupiesDAC: true, Primary: true,
		Description: "Roon RAAT 终端代理（位完美播放）。设备与音量在 Roon Core 中配置。",
	},
	"networkaudiod": {
		AppID: "networkaudiod", Name: "HQPlayer NAA", Protocol: "naa", Category: "renderer",
		Units: []string{"networkaudiod.service"}, OccupiesDAC: true, Primary: true,
		Description: "HQPlayer Network Audio Adapter。输出设备由 HQPlayer 服务端指定。",
	},
	"shairport-sync": {
		AppID: "shairport-sync", Name: "AirPlay", Protocol: "airplay", Category: "renderer",
		Units: []string{"shairport-sync.service"}, OccupiesDAC: true, Primary: true,
		DepsPkgs:    []string{"avahi-daemon", "libasound2", "libsoxr0", "libconfig11"},
		ConfigPath:  "/etc/shairport-sync.conf",
		Description: "AirPlay 音频渲染（AirPlay 2 需 nqptp 时钟）。家人用 iPhone 隔空播放。",
		ConfigFields: []ConfigField{
			{Key: "airplay_name", Label: "AirPlay 名称", Default: "HiFi 音箱", Placeholder: "客厅音箱", Help: "iPhone 隔空播放列表里显示的名字"},
			{Key: "dac_device", Label: "DAC 设备", Default: "hw:1,0", Placeholder: "hw:1,0", Help: "ALSA 输出设备；hw: 直通位完美，plughw: 允许重采样"},
			{Key: "port", Label: "端口", Default: "7000", Placeholder: "7000", Help: "AirPlay 2 默认 7000，AirPlay 1 为 5000"},
		},
		ConfigTemplate: `general =
{
  name = "{{.airplay_name}}";
  port = {{.port}};
  interpolation = "soxr";
  output_backend = "alsa";
};
alsa =
{
  output_device = "{{.dac_device}}";
  mixer_control_name = "PCM";
};
`,
	},
	"nqptp": {
		AppID: "nqptp", Name: "nqptp (AirPlay 2 时钟)", Category: "dependency",
		Units: []string{"nqptp.service"}, OccupiesDAC: false, HelperFor: "shairport-sync",
		Description: "AirPlay 2 PTP 时钟同步守护进程，AirPlay 2 必需。",
	},
	"upmpdcli": {
		AppID: "upmpdcli", Name: "UPnP / DLNA", Protocol: "upnp", Category: "renderer",
		Units: []string{"upmpdcli.service"}, OccupiesDAC: true, Primary: true,
		DepsPkgs:    []string{"mpd", "libasound2"},
		ConfigPath:  "/etc/upmpdcli.conf",
		Description: "UPnP/DLNA MediaRenderer（兼容 OpenHome ohMedia），播放后端为 MPD。",
		ConfigFields: []ConfigField{
			{Key: "room", Label: "房间名", Default: "客厅", Placeholder: "客厅", Help: "DLNA 控制点（BubbleUPnP、Kazoo 等）投送列表里显示的名字"},
			{Key: "port", Label: "UPnP 端口", Default: "49152", Placeholder: "49152", Help: "同机多实例必须唯一"},
			{Key: "dac_device", Label: "DAC 设备", Default: "hw:1,0", Placeholder: "hw:1,0", Help: "MPD 的 ALSA 输出设备"},
		},
		ConfigTemplate: `friendlyname = {{.room}}
ohproductroom = {{.room}}
upnpport = {{.port}}
mpdhost = localhost
mpdport = 6600
`,
	},
	"mpd": {
		AppID: "mpd", Name: "MPD (播放后端)", Category: "dependency",
		Units: []string{"mpd.service"}, OccupiesDAC: true, HelperFor: "upmpdcli",
		ConfigPath:  "/etc/mpd.conf",
		Description: "Music Player Daemon，upmpdcli（UPnP/DLNA）的播放后端。",
		ConfigFields: []ConfigField{
			{Key: "dac_device", Label: "DAC 设备", Default: "hw:1,0", Placeholder: "hw:1,0"},
		},
		ConfigTemplate: `music_directory "/var/lib/mpd/music"
playlist_directory "/var/lib/mpd/playlists"
db_file "/var/lib/mpd/tag_cache"
state_file "/var/lib/mpd/state"
sticker_file "/var/lib/mpd/sticker.sql"
user "mpd"
bind_to_address "localhost"
port "6600"
audio_output {
  type "alsa"
  name "DAC"
  device "{{.dac_device}}"
  mixer_type "none"
}
`,
	},
	"avahi-daemon": {
		AppID: "avahi-daemon", Name: "Avahi (mDNS)", Category: "dependency",
		Units: []string{"avahi-daemon.service"}, OccupiesDAC: false, HelperFor: "shairport-sync",
		Description: "mDNS/DNS-SD 广播，AirPlay 与 RAAT 发现必需。",
	},
	"spotifyd": {
		AppID: "spotifyd", Name: "Spotify Connect", Protocol: "spotify", Category: "renderer",
		Units: []string{"spotifyd.service"}, OccupiesDAC: true,
		Description: "Spotify Connect 渲染端。仅在终端本地检测到时显示。",
	},
	"snapclient": {
		AppID: "snapclient", Name: "Snapcast 客户端", Protocol: "snapcast", Category: "renderer",
		Units: []string{"snapclient.service"}, OccupiesDAC: true,
		Description: "Snapcast 多房同步播放客户端。仅在终端本地检测到时显示。",
	},
	"squeezelite": {
		AppID: "squeezelite", Name: "Squeezelite (LMS)", Protocol: "slimproto", Category: "renderer",
		Units: []string{"squeezelite.service"}, OccupiesDAC: true,
		Description: "Logitech Media Server / Slimproto 渲染端。仅在终端本地检测到时显示。",
	},
	"gmediarenderer": {
		AppID: "gmediarenderer", Name: "UPnP/DLNA (gmrender)", Protocol: "upnp", Category: "renderer",
		Units: []string{"gmediarenderer.service"}, OccupiesDAC: true,
		Description: "gmrender-resurrect UPnP/DLNA 渲染端。仅在终端本地检测到时显示。",
	},
}

// PrimaryServices returns the top-level card services in display order.
func PrimaryServices() []ServiceDef {
	order := []string{"roonbridge", "networkaudiod", "shairport-sync", "upmpdcli"}
	out := make([]ServiceDef, 0, len(order))
	for _, id := range order {
		out = append(out, Registry[id])
	}
	return out
}

// AutoServices returns non-primary audio services that render as cards only
// when detected on the terminal itself (live process or loaded systemd unit):
// protocol renderers and playback backends the terminal image may already run.
func AutoServices() []ServiceDef {
	order := []string{"mpd", "gmediarenderer", "snapclient", "spotifyd", "squeezelite"}
	out := make([]ServiceDef, 0, len(order))
	for _, id := range order {
		out = append(out, Registry[id])
	}
	return out
}

// HelpersFor returns the helper app IDs required by a primary service.
func HelpersFor(appID string) []string {
	var out []string
	for id, def := range Registry {
		if def.HelperFor == appID {
			out = append(out, id)
		}
	}
	return out
}
