package androidcore

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Handler returns the Android terminal's local web console: a focused now-playing
// view with transport controls, node identity and live metrics. It is served on
// loopback and shown in the app's WebView. Only the feasible Android subset is
// exposed (no app installation, systemd or power management).
func Handler(c *Core) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(consoleHTML))
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		writeJSON(w, map[string]any{
			"identity": c.Identity(ctx),
			"playback": c.Playback(),
			"metrics":  c.Metrics(),
		})
	})
	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) {
		paused, err := c.Pause()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"paused": paused})
	})
	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := c.Stop(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/seek", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PTS uint64 `json:"pts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, err)
			return
		}
		if err := c.Seek(body.PTS); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/volume", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Volume int `json:"volume"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, err)
			return
		}
		if err := c.Volume(body.Volume); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ServeConsole runs the console HTTP server on addr until ctx is cancelled.
func ServeConsole(ctx context.Context, c *Core, addr string) error {
	srv := &http.Server{Addr: addr, Handler: Handler(c)}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

const consoleHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Zephyr Music Player · 终端</title>
<style>
:root{
  --bg0:#0b1020; --bg1:#131a33; --card:#161d3a; --line:#26305c;
  --tx:#e8ecff; --mut:#9aa6d6; --acc:#6ea8fe; --acc2:#9b8cff; --ok:#54d6a0; --warn:#ffcf6e;
}
*{box-sizing:border-box}
html,body{margin:0;height:100%}
body{
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;
  color:var(--tx); background:
    radial-gradient(1200px 600px at 10% -10%, #2a3566 0%, transparent 55%),
    radial-gradient(900px 500px at 110% 10%, #3a2a66 0%, transparent 50%),
    linear-gradient(160deg,var(--bg0),var(--bg1));
  background-attachment:fixed; -webkit-font-smoothing:antialiased;
}
.wrap{max-width:560px;margin:0 auto;padding:18px 16px 40px}
header{display:flex;align-items:center;gap:10px;margin:6px 2px 16px}
.logo{width:34px;height:34px;border-radius:9px;background:linear-gradient(135deg,var(--acc),var(--acc2));
  display:flex;align-items:center;justify-content:center;font-weight:800;color:#0b1020}
.brand{font-weight:700;letter-spacing:.3px}
.brand small{display:block;color:var(--mut);font-weight:500;font-size:11px;letter-spacing:.6px}
.card{background:linear-gradient(180deg,rgba(255,255,255,.04),rgba(255,255,255,.01)),var(--card);
  border:1px solid var(--line);border-radius:16px;padding:16px;margin-bottom:14px;
  box-shadow:0 10px 30px rgba(0,0,0,.25)}
.row{display:flex;align-items:center;justify-content:space-between;gap:10px}
.mut{color:var(--mut)}
.pill{font-size:11px;padding:3px 9px;border-radius:999px;border:1px solid var(--line);color:var(--mut)}
.pill.on{color:#06281c;background:var(--ok);border-color:transparent;font-weight:700}
.pill.off{color:#2a2106;background:var(--warn);border-color:transparent;font-weight:700}
.title{font-size:20px;font-weight:700;margin:2px 0 4px;line-height:1.3}
.artist{color:var(--mut);font-size:14px}
.meta{display:flex;flex-wrap:wrap;gap:6px;margin-top:12px}
.chip{font-size:11px;color:var(--mut);border:1px solid var(--line);border-radius:8px;padding:3px 8px}
.ctl{display:flex;gap:10px;margin-top:16px}
button{flex:1;appearance:none;border:1px solid var(--line);background:rgba(255,255,255,.05);
  color:var(--tx);font-size:15px;font-weight:600;padding:13px 10px;border-radius:12px;cursor:pointer;
  transition:transform .06s ease,background .2s}
button:active{transform:scale(.97)}
button.primary{background:linear-gradient(135deg,var(--acc),var(--acc2));color:#0b1020;border-color:transparent}
button:disabled{opacity:.45;cursor:default}
.slider{margin-top:16px}
.slider label{display:flex;justify-content:space-between;font-size:12px;color:var(--mut);margin-bottom:6px}
input[type=range]{width:100%;accent-color:var(--acc)}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.kv{font-size:13px}
.kv b{display:block;font-size:18px;font-weight:700;margin-top:2px}
h3{font-size:12px;letter-spacing:.8px;text-transform:uppercase;color:var(--mut);margin:0 0 10px}
.foot{text-align:center;color:var(--mut);font-size:11px;margin-top:18px}
.dot{width:8px;height:8px;border-radius:50%;display:inline-block;margin-right:6px;background:var(--mut)}
.dot.on{background:var(--ok)} .dot.off{background:var(--warn)}
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="logo">Z</div>
    <div class="brand">Zephyr Music Player<small>ANDROID 终端 · ZAT 播放</small></div>
  </header>

  <div class="card">
    <div class="row">
      <span class="mut" id="conn"><span class="dot"></span>未连接</span>
      <span class="pill" id="state">idle</span>
    </div>
    <div class="title" id="title">等待播放</div>
    <div class="artist" id="artist">服务端推送 ZAT 音频后开始播放</div>
    <div class="meta" id="meta"></div>
    <div class="ctl">
      <button class="primary" id="pause">暂停</button>
      <button id="stop">停止</button>
    </div>
    <div class="slider">
      <label><span>进度</span><span id="ptslab">0</span></label>
      <input type="range" id="seek" min="0" max="100" value="0">
    </div>
    <div class="slider">
      <label><span>音量</span><span id="vollab">100</span></label>
      <input type="range" id="vol" min="0" max="100" value="100">
    </div>
  </div>

  <div class="card">
    <h3>终端</h3>
    <div class="grid">
      <div class="kv mut">别名<b id="alias">—</b></div>
      <div class="kv mut">节点<b id="node" style="font-size:13px">—</b></div>
      <div class="kv mut">服务端<b id="server" style="font-size:13px">—</b></div>
      <div class="kv mut">注册<b id="reg">—</b></div>
    </div>
  </div>

  <div class="card">
    <h3>资源</h3>
    <div class="grid">
      <div class="kv mut">CPU<b id="cpu">—</b></div>
      <div class="kv mut">内存<b id="mem">—</b></div>
      <div class="kv mut">温度<b id="temp">—</b></div>
      <div class="kv mut">缓冲<b id="buf">—</b></div>
    </div>
  </div>

  <div class="foot">Zephyr Music Player · 比特完美 ZAT 终端 · AAudio 输出</div>
</div>
<script>
var seekMax = 0, seeking = false, volBusy = false;
function el(id){return document.getElementById(id);}
function post(path, body){
  return fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body||{})})
    .then(function(r){return r.json();});
}
function chip(t){var s=document.createElement('span');s.className='chip';s.textContent=t;return s;}
function render(d){
  var id=d.identity||{}, p=d.playback||{}, m=d.metrics||{};
  var conn=el('conn');
  conn.innerHTML='<span class="dot '+(p.connected?'on':'off')+'"></span>'+(p.connected?'已连接服务端':'未连接');
  el('state').textContent=p.state||'idle';
  el('title').textContent=p.title||'等待播放';
  el('artist').textContent=(p.artist||'')+(p.album?' · '+p.album:'')||(p.connected?'服务端推送 ZAT 音频后开始播放':'');
  var meta=el('meta'); meta.innerHTML='';
  if(p.codec)meta.appendChild(chip(p.codec));
  if(p.rate)meta.appendChild(chip((p.rate/1000)+' kHz'));
  if(p.channels)meta.appendChild(chip(p.channels+' ch'));
  if(p.format)meta.appendChild(chip(p.format));
  el('alias').textContent=id.alias||'—';
  el('node').textContent=id.node_id||'未注册';
  el('server').textContent=id.server_url||'—';
  el('reg').innerHTML=id.registered?'<span class="pill on">已注册</span>':'<span class="pill off">未注册</span>';
  el('cpu').textContent=(m.cpu_percent!=null?m.cpu_percent.toFixed(0):'—')+'%';
  el('mem').textContent=(m.mem_used_mb!=null?m.mem_used_mb.toFixed(0):'—')+' MB';
  el('temp').textContent=(m.temp_c?m.temp_c.toFixed(0)+'°C':'—');
  el('buf').textContent=(p.buffer_ms!=null?p.buffer_ms:0)+' ms';
  el('pause').textContent=(p.state==='paused')?'继续':'暂停';
  if(!seeking){
    var dur=p.duration_ms||0;
    seekMax=dur>0?Math.floor(dur*(p.rate||44100)/1000):0;
    var sk=el('seek'); sk.max=seekMax>0?seekMax:100; sk.value=p.pts||0;
    el('ptslab').textContent=seekMax>0?((p.pts||0)+' / '+seekMax):(p.pts||0);
  }
  if(!volBusy){el('vol').value=p.volume!=null?p.volume:100;el('vollab').textContent=(p.volume!=null?p.volume:100);}
}
function tick(){fetch('/api/state').then(function(r){return r.json();}).then(render).catch(function(){});}
el('pause').onclick=function(){post('/api/pause').then(tick);};
el('stop').onclick=function(){post('/api/stop').then(tick);};
el('seek').addEventListener('input',function(){seeking=true;el('ptslab').textContent=this.value;});
el('seek').addEventListener('change',function(){seeking=false;post('/api/seek',{pts:parseInt(this.value,10)}).then(tick);});
el('vol').addEventListener('input',function(){volBusy=true;el('vollab').textContent=this.value;});
el('vol').addEventListener('change',function(){var v=parseInt(this.value,10);post('/api/volume',{volume:v}).then(function(){volBusy=false;tick();});});
tick(); setInterval(tick,1000);
</script>
</body>
</html>`
