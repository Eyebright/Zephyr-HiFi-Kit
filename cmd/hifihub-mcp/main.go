// Command hifihub-mcp exposes Zephyr HiFi Kit player control as a stdio MCP
// server so external agents (Qoder, WorkBuddy, ...) can browse the library and
// drive playback through the existing admin HTTP API.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const protocolVersion = "2025-06-18"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	base := strings.TrimRight(envOr("HIFIHUB_BASE_URL", "http://127.0.0.1:8080"), "/")
	c := &client{base: base, cookie: os.Getenv("HIFIHUB_SESSION_COOKIE"), http: &http.Client{Timeout: 60 * time.Second}}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification
		}
		result, rerr := dispatch(c, req.Method, req.Params)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rerr != nil {
			resp["error"] = rerr
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			fmt.Fprintln(os.Stderr, "hifihub-mcp: write:", err)
			return
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func dispatch(c *client, method string, params json.RawMessage) (any, *rpcErr) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "hifihub-player", "version": "0.1.0"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcErr{-32602, "invalid params"}
		}
		for _, t := range tools {
			if t.name != p.Name {
				continue
			}
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			m, path, q, body := t.call(p.Arguments)
			status, data, err := c.do(m, path, q, body)
			if err != nil {
				return toolText(fmt.Sprintf(`{"error":%q}`, err.Error()), true), nil
			}
			return toolText(string(data), status >= 400), nil
		}
		return nil, &rpcErr{-32602, "unknown tool: " + p.Name}
	}
	return nil, &rpcErr{-32601, "method not found: " + method}
}

func toolText(text string, isErr bool) any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
}

// client talks to the server admin API. Loopback deployments auto-authenticate
// via GET /api/v1/settings/session; remote deployments pass a session cookie
// through HIFIHUB_SESSION_COOKIE.
type client struct {
	base   string
	cookie string
	http   *http.Client
}

func (c *client) login() error {
	resp, err := c.http.Get(c.base + "/api/v1/settings/session")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	for _, ck := range resp.Cookies() {
		if ck.Value != "" {
			c.cookie = ck.Name + "=" + ck.Value
			return nil
		}
	}
	return fmt.Errorf("no session cookie (remote hosts need HIFIHUB_SESSION_COOKIE)")
}

func (c *client) do(method, path string, q url.Values, body any) (int, []byte, error) {
	if c.cookie == "" {
		if err := c.login(); err != nil {
			return 0, nil, err
		}
	}
	for attempt := 0; ; attempt++ {
		u := c.base + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return 0, nil, err
			}
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, u, rdr)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Origin", c.base)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Cookie", c.cookie)
		if body != nil || method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return 0, nil, err
		}
		if (resp.StatusCode == 401 || resp.StatusCode == 403) && attempt == 0 && strings.Contains(string(data), "settings_login_required") {
			if err := c.login(); err != nil {
				return resp.StatusCode, data, nil
			}
			continue
		}
		return resp.StatusCode, data, nil
	}
}

type tool struct {
	name   string
	desc   string
	schema map[string]any
	call   func(a map[string]any) (method, path string, q url.Values, body any)
}

func str(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}

func num(a map[string]any, k string) float64 {
	n, _ := a[k].(float64)
	return n
}

func nodePath(a map[string]any, suffix string) string {
	return "/api/v1/nodes/" + url.PathEscape(str(a, "node_id")) + suffix
}

func devQuery(a map[string]any) url.Values {
	q := url.Values{}
	if dk := str(a, "device_key"); dk != "" {
		q.Set("device_key", dk)
	}
	return q
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func sProp(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func nProp(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

const deviceKeyDoc = "播放设备键，格式 <node_id>::<hw>（如 n-xxx::hw:3,0）；省略时用终端当前活动设备"

var tools = []tool{
	{"list_nodes", "列出所有终端节点（node_id、别名、在线状态、agent 版本）", obj(map[string]any{}),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", "/api/v1/nodes", nil, nil
		}},
	{"list_playback_devices", "列出当前可播放的激活设备及其播放状态", obj(map[string]any{}),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", "/api/v1/playback/devices", nil, nil
		}},
	{"list_devices", "列出某终端上报的 ALSA 播放设备（hw 标识、名称、启用态）", obj(map[string]any{"node_id": sProp("终端 node_id")}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", nodePath(a, "/devices"), nil, nil
		}},
	{"player_status", "查询播放状态：曲目、位置 pts、缓冲、音量、设备、暂停态", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", nodePath(a, "/play"), devQuery(a), nil
		}},
	{"play_track", "推送曲目到终端立即播放（替换队列为该曲）", obj(map[string]any{"node_id": sProp("终端 node_id"), "track_id": sProp("曲库 track id"), "device_key": sProp(deviceKeyDoc)}, "node_id", "track_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/play"), nil, map[string]any{"track_id": str(a, "track_id"), "device_key": str(a, "device_key")}
		}},
	{"pause", "暂停播放", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/pause"), devQuery(a), map[string]any{}
		}},
	{"resume", "恢复播放", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/resume"), devQuery(a), map[string]any{}
		}},
	{"stop", "停止播放并清空终端缓冲", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/stop"), devQuery(a), map[string]any{}
		}},
	{"seek", "跳转到指定毫秒位置", obj(map[string]any{"node_id": sProp("终端 node_id"), "position_ms": nProp("目标位置（毫秒）"), "device_key": sProp(deviceKeyDoc)}, "node_id", "position_ms"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/seek"), nil, map[string]any{"position_ms": int(num(a, "position_ms")), "device_key": str(a, "device_key")}
		}},
	{"set_volume", "设置音量 0-100（位完美直通时终端锁定 100）", obj(map[string]any{"node_id": sProp("终端 node_id"), "volume": nProp("0-100"), "device_key": sProp(deviceKeyDoc)}, "node_id", "volume"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/volume"), nil, map[string]any{"volume": int(num(a, "volume")), "device_key": str(a, "device_key")}
		}},
	{"select_device", "切换终端的播放设备（hw 标识，需已激活）", obj(map[string]any{"node_id": sProp("终端 node_id"), "device": sProp("ALSA hw 标识，如 hw:3,0"), "device_key": sProp(deviceKeyDoc)}, "node_id", "device"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/device"), nil, map[string]any{"device": str(a, "device"), "device_key": str(a, "device_key")}
		}},
	{"queue_get", "查询设备播放队列与当前位置", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", nodePath(a, "/queue"), devQuery(a), nil
		}},
	{"queue_set", "整队替换；play_if_idle=true 时空闲立即开播", obj(map[string]any{"node_id": sProp("终端 node_id"), "track_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "曲目 id 列表"}, "position": nProp("起播索引"), "play_if_idle": map[string]any{"type": "boolean"}, "device_key": sProp(deviceKeyDoc)}, "node_id", "track_ids"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "PUT", nodePath(a, "/queue"), nil, a
		}},
	{"queue_enqueue", "追加单曲到队尾", obj(map[string]any{"node_id": sProp("终端 node_id"), "track_id": sProp("曲目 id"), "device_key": sProp(deviceKeyDoc)}, "node_id", "track_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/queue/enqueue"), nil, map[string]any{"track_id": str(a, "track_id"), "device_key": str(a, "device_key")}
		}},
	{"queue_next", "下一首", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/queue/next"), devQuery(a), map[string]any{}
		}},
	{"queue_prev", "上一首", obj(map[string]any{"node_id": sProp("终端 node_id"), "device_key": sProp(deviceKeyDoc)}, "node_id"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/queue/prev"), devQuery(a), map[string]any{}
		}},
	{"queue_jump", "跳到队列中指定索引", obj(map[string]any{"node_id": sProp("终端 node_id"), "index": nProp("队列索引"), "device_key": sProp(deviceKeyDoc)}, "node_id", "index"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/queue/jump"), devQuery(a), map[string]any{"index": int(num(a, "index"))}
		}},
	{"queue_mode", "设置播放模式 sequence/loop_all/loop_one/shuffle", obj(map[string]any{"node_id": sProp("终端 node_id"), "mode": sProp("sequence|loop_all|loop_one|shuffle"), "device_key": sProp(deviceKeyDoc)}, "node_id", "mode"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "POST", nodePath(a, "/queue/mode"), nil, map[string]any{"mode": str(a, "mode"), "device_key": str(a, "device_key")}
		}},
	{"search_library", "检索曲库：category=albums/tracks/artists/genres/years，q 关键词，返回带 track id 的分页结果", obj(map[string]any{"q": sProp("关键词"), "category": sProp("albums|tracks|artists|genres|years|composers|folders"), "page": nProp("页码，从 1 起"), "page_size": nProp("每页条数"), "artist": sProp("艺术家过滤"), "album": sProp("专辑过滤")}),
		func(a map[string]any) (string, string, url.Values, any) {
			q := url.Values{}
			for _, k := range []string{"q", "category", "artist", "album"} {
				if v := str(a, k); v != "" {
					q.Set(k, v)
				}
			}
			for _, k := range []string{"page", "page_size"} {
				if v := num(a, k); v > 0 {
					q.Set(k, fmt.Sprintf("%d", int(v)))
				}
			}
			return "GET", "/api/v1/library/browse", q, nil
		}},
	{"eq_get", "读取设备 EQ/DSP 配置（16 段图形或 PEQ、前置增益、启用态）", obj(map[string]any{"node_id": sProp("终端 node_id"), "hw": sProp("ALSA hw 标识，如 hw:3,0")}, "node_id", "hw"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "GET", nodePath(a, "/devices/"+url.PathEscape(str(a, "hw"))+"/dsp"), nil, nil
		}},
	{"eq_set", "写入设备 EQ/DSP 配置；config 为完整配置对象（enabled/preset/preamp/bands/mode/peq）", obj(map[string]any{"node_id": sProp("终端 node_id"), "hw": sProp("ALSA hw 标识"), "config": map[string]any{"type": "object", "description": "EQ 配置对象"}}, "node_id", "hw", "config"),
		func(a map[string]any) (string, string, url.Values, any) {
			return "PUT", nodePath(a, "/devices/"+url.PathEscape(str(a, "hw"))+"/dsp"), nil, a["config"]
		}},
}

func toolDefs() []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{"name": t.name, "description": t.desc, "inputSchema": t.schema})
	}
	return out
}