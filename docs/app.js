/* ============================================================
   Zephyr Music Player · Product Site
   Single rAF loop · shared fleet state · WebAudio ambience
   ============================================================ */
(function () {
  "use strict";
  var $ = function (s, r) { return (r || document).querySelector(s); };
  var $$ = function (s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); };
  var clamp = function (v, a, b) { return v < a ? a : v > b ? b : v; };
  var lerp = function (a, b, t) { return a + (b - a) * t; };
  var rnd = function (a, b) { return a + Math.random() * (b - a); };
  var pick = function (a) { return a[(Math.random() * a.length) | 0]; };
  var reduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  var HUES = {
    amber: { key: "#f6a821", lit: "#ffd479", sig: "#ff8a3d" },
    cyan: { key: "#34d3eb", lit: "#a8e9f5", sig: "#4aa8ff" },
    magenta: { key: "#ff5db1", lit: "#ffa8d4", sig: "#9b8cff" },
    green: { key: "#46e08a", lit: "#a9f5cd", sig: "#34d3eb" }
  };
  var C = { online: "#46e08a", play: "#f6a821", off: "#ff6b6b", upd: "#34d3eb", idle: "#5d6d83", sel: "#ffffff" };

  /* ─────────── toast ─────────── */
  var toastRoot = $("#toast-root");
  function toast(msg, kind) {
    if (!toastRoot) return;
    var el = document.createElement("div");
    el.className = "toast " + (kind || "info");
    el.textContent = msg;
    toastRoot.appendChild(el);
    setTimeout(function () {
      el.classList.add("out");
      setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); }, 280);
    }, 2600);
    while (toastRoot.children.length > 4) toastRoot.removeChild(toastRoot.firstChild);
  }

  /* ─────────── audio ─────────── */
  // BGM: 《故乡的小路》 (陈克正 词 / 崔蕾 曲), 1=F, 7/4 (3/4+4/4), ♩=72.
  // Transcribed melody (top voice), encoded as [semitone-from-1, octave, beats].
  // Semitones: 1=0 2=2 3=4 4=5 #4=6 5=7 6=9 7=11. Rendered as a soft music box.
  var BGM_SPB = 60 / 72;
  var BGM = [
    /* 故乡的小路，是我童年走过的路， */
    [4,0,1.5],[4,0,.25],[4,0,.25],[4,0,.5],[2,0,.5],[0,0,3],[4,0,.5],[5,0,.5],
    [7,0,1.5],[6,0,.5],[9,0,.5],[7,0,.25],[7,0,.25],[4,0,3],[7,-1,.5],[0,0,.5],
    /* 路旁盛开的小花，给我欢乐和幸福， */
    [4,0,1.5],[4,0,.25],[4,0,.25],[5,0,.5],[4,0,.5],[2,0,3],[4,0,.5],[5,0,.5],
    [7,0,1.5],[7,0,.25],[7,0,.25],[2,0,.5],[0,0,.5],[0,0,3],[0,0,1],
    /* 弯弯的小路，梦中我恩念的路， */
    [9,0,1.5],[9,0,.25],[9,0,.25],[11,0,.5],[0,1,.5],[7,0,3],[9,0,.5],[7,0,.5],
    [4,0,1.5],[7,-1,.5],[0,0,.5],[2,0,.5],[4,0,3],[0,0,1],
    /* 路旁的小花，时刻向我倾诉。 */
    [9,0,1.5],[9,0,.25],[9,0,.25],[11,0,.5],[0,1,.5],[7,0,3],[5,0,.5],[4,0,.5],
    [2,0,1.5],[7,-1,.5],[2,0,.5],[0,0,.5],[0,0,3],[0,0,1],
    [null,0,2]
  ];
  var BGM_BASE = 349.228; // F4 = degree 1
  var Audio2 = {
    ctx: null, master: null, analyser: null, data: null, on: true, started: false, nodes: [],
    musicGain: null, musicTimer: null, mIdx: 0, mNext: 0, mPlayed: 0,
    init: function () {
      if (this.ctx) return;
      var AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) { this.on = false; return; }
      try { this.ctx = new AC(); } catch (e) { this.on = false; return; }
      this.master = this.ctx.createGain();
      this.master.gain.value = this.on ? 0.12 : 0;
      this.analyser = this.ctx.createAnalyser();
      this.analyser.fftSize = 128;
      this.analyser.smoothingTimeConstant = 0.82;
      this.data = new Uint8Array(this.analyser.frequencyBinCount);
      var lp = this.ctx.createBiquadFilter();
      lp.type = "lowpass"; lp.frequency.value = 760; lp.Q.value = 0.7;
      this.lp = lp;
      lp.connect(this.analyser);
      this.analyser.connect(this.master);
      this.master.connect(this.ctx.destination);
      // F-major bed (F2 C3 F3 A3 C4) so the ambient pad is consonant with the
      // 《故乡的小路》 melody (1=F) instead of the previous A-major drone.
      var freqs = [87.31, 130.81, 174.61, 220, 261.63];
      var self = this;
      freqs.forEach(function (f, i) {
        var o = self.ctx.createOscillator();
        o.type = i % 2 ? "triangle" : "sine";
        o.frequency.value = f;
        o.detune.value = (i - 2) * 4;
        var g = self.ctx.createGain();
        g.gain.value = 0.10 / (i + 1);
        var lfo = self.ctx.createOscillator();
        lfo.frequency.value = 0.05 + i * 0.021;
        var lg = self.ctx.createGain();
        lg.gain.value = 0.05 / (i + 1);
        lfo.connect(lg); lg.connect(g.gain);
        o.connect(g); g.connect(lp);
        o.start(); lfo.start();
        self.nodes.push(o, lfo);
      });
      this.musicGain = this.ctx.createGain();
      this.musicGain.gain.value = 1;
      this.musicGain.connect(this.analyser);
      this.started = true;
      this.startMusic();
    },
    startMusic: function () {
      if (this.musicTimer || !this.ctx) return;
      var self = this;
      this.mNext = 0;
      this.musicTimer = setInterval(function () { self.tickMusic(); }, 120);
    },
    tickMusic: function () {
      if (!this.ctx || this.ctx.state !== "running") { this.mNext = 0; return; }
      var now = this.ctx.currentTime;
      if (!this.mNext || this.mNext < now - 0.25) this.mNext = now + 0.08;
      while (this.mNext < now + 0.7) {
        var n = BGM[this.mIdx];
        if (n[0] !== null) this.noteMusic(n, this.mNext);
        this.mNext += n[2] * BGM_SPB;
        this.mIdx = (this.mIdx + 1) % BGM.length;
      }
    },
    noteMusic: function (n, t) {
      var f = BGM_BASE * Math.pow(2, (n[0] + 12 * n[1]) / 12);
      var dur = n[2] * BGM_SPB;
      var decay = Math.max(0.6, dur * 1.5);
      var v = 0.5;
      var partials = [[1, 1, decay], [2, 0.32, decay * 0.6], [4, 0.10, decay * 0.3]];
      for (var i = 0; i < partials.length; i++) {
        var o = this.ctx.createOscillator(), g = this.ctx.createGain();
        o.type = "sine";
        o.frequency.value = f * partials[i][0];
        var pv = v * partials[i][1], pd = partials[i][2];
        g.gain.setValueAtTime(0.0001, t);
        g.gain.exponentialRampToValueAtTime(pv, t + 0.008);
        g.gain.exponentialRampToValueAtTime(0.0001, t + pd);
        o.connect(g); g.connect(this.musicGain);
        o.start(t); o.stop(t + pd + 0.05);
      }
      this.mPlayed++;
    },
    resume: function () {
      this.init();
      if (this.ctx && this.ctx.state === "suspended") this.ctx.resume().catch(function () {});
    },
    setOn: function (v) {
      this.on = v;
      if (v) this.resume();
      if (this.master && this.ctx) {
        var t = this.ctx.currentTime;
        this.master.gain.cancelScheduledValues(t);
        this.master.gain.setTargetAtTime(v ? 0.12 : 0, t, 0.12);
      }
    },
    blip: function (freq, dur, type, vol) {
      if (!this.on || !this.ctx) return;
      var t = this.ctx.currentTime;
      var o = this.ctx.createOscillator(), g = this.ctx.createGain();
      o.type = type || "sine"; o.frequency.setValueAtTime(freq, t);
      o.frequency.exponentialRampToValueAtTime(Math.max(40, freq * 0.62), t + dur);
      g.gain.setValueAtTime(0.0001, t);
      g.gain.exponentialRampToValueAtTime(vol || 0.16, t + 0.012);
      g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      o.connect(g); g.connect(this.master);
      o.start(t); o.stop(t + dur + 0.03);
    },
    spectrum: function () {
      if (this.analyser && this.on && this.started) { this.analyser.getByteFrequencyData(this.data); return this.data; }
      return null;
    }
  };
  var sndBtn = $("#snd"), sndTxt = $("#snd-txt");
  function paintSnd() {
    if (!sndBtn) return;
    sndBtn.setAttribute("aria-pressed", Audio2.on ? "true" : "false");
    if (sndTxt) sndTxt.textContent = Audio2.on ? "声音 开" : "声音 关";
  }
  if (sndBtn) sndBtn.addEventListener("click", function () {
    Audio2.setOn(!Audio2.on); paintSnd();
    toast(Audio2.on ? "声音已开启" : "全部声音已静音", Audio2.on ? "ok" : "info");
  });
  paintSnd();
  var bgmAnnounced = false;
  var unlock = function () {
    Audio2.resume();
    if (!bgmAnnounced && Audio2.on) {
      bgmAnnounced = true;
      toast("背景音乐：《故乡的小路》", "info");
    }
    document.removeEventListener("pointerdown", unlock);
    document.removeEventListener("keydown", unlock);
  };
  document.addEventListener("pointerdown", unlock, { once: false });
  document.addEventListener("keydown", unlock, { once: false });
  window.__za = Audio2;

  /* ─────────── nav ─────────── */
  var nav = $("#nav"), prog = $("#nav-prog"), burger = $("#burger"), links = $("#nav-links");
  var spyLinks = $$("#nav-links a[data-spy]");
  var sections = spyLinks.map(function (a) { return document.getElementById(a.dataset.spy); }).filter(Boolean);
  function onScroll() {
    var y = window.scrollY || window.pageYOffset;
    if (nav) nav.classList.toggle("stuck", y > 12);
    var h = document.documentElement.scrollHeight - window.innerHeight;
    if (prog) prog.style.width = (h > 0 ? clamp(y / h, 0, 1) * 100 : 0) + "%";
    var tt = $("#totop"); if (tt) tt.hidden = y < 620;
    var cur = null, probe = y + (window.innerHeight * 0.32);
    for (var i = 0; i < sections.length; i++) if (sections[i].offsetTop <= probe) cur = sections[i].id;
    spyLinks.forEach(function (a) { a.classList.toggle("on", a.dataset.spy === cur); });
  }
  window.addEventListener("scroll", onScroll, { passive: true });
  window.addEventListener("resize", onScroll);
  if (burger && links) burger.addEventListener("click", function () {
    var open = links.classList.toggle("open");
    burger.setAttribute("aria-expanded", open ? "true" : "false");
  });
  $$("#nav-links a").forEach(function (a) {
    a.addEventListener("click", function () {
      if (links) links.classList.remove("open");
      if (burger) burger.setAttribute("aria-expanded", "false");
    });
  });
  var tt = $("#totop");
  if (tt) tt.addEventListener("click", function () { window.scrollTo({ top: 0, behavior: reduced ? "auto" : "smooth" }); });

  /* ─────────── reveal ─────────── */
  if ("IntersectionObserver" in window) {
    var ro = new IntersectionObserver(function (es) {
      es.forEach(function (e) { if (e.isIntersecting) { e.target.classList.add("in"); ro.unobserve(e.target); } });
    }, { rootMargin: "0px 0px -8% 0px", threshold: 0.06 });
    $$(".rv").forEach(function (el, i) {
      if (!el.style.getPropertyValue("--d")) el.style.setProperty("--d", (Math.min(i, 6) * 55) + "ms");
      ro.observe(el);
    });
  } else $$(".rv").forEach(function (el) { el.classList.add("in"); });

  /* ─────────── marquee ─────────── */
  var MQ = ["GPG 强制验签", "BOM 期望态", "灰度环推进", "播放门控 FR-3.10", "席位授权", "全量审计", "DAC 仲裁",
    "一次性注册令牌", "systemd 白名单", "mDNS / SSDP 诊断", "版本历史回滚", "CSV / JSON 导出", "命令通道", "信任根分工",
    "SMB 媒体库", "ZAT 推流协议", "libFLAC 解码", "ALSA 比特完美", "音频格式识别", "正在播放控制台"];
  var mq = $("#mq-track");
  if (mq) {
    var html = MQ.map(function (t) { return '<span class="mq-i"><b>' + t + "</b></span>"; }).join("");
    mq.innerHTML = html + html;
  }

  /* ─────────── feature pipeline ─────────── */
  var STATES = ["precheck", "resolve_deps", "waiting_idle", "downloading", "verifying", "installing", "rendering_config", "enabling_units", "validating", "done"];
  var fp = $("#feat-pipeline"), fpIdx = 0;
  if (fp) {
    fp.innerHTML = STATES.map(function (s) { return '<span class="pl-s">' + s + "</span>"; }).join("");
    if (!reduced) setInterval(function () {
      var kids = fp.children;
      for (var i = 0; i < kids.length; i++) {
        kids[i].classList.toggle("hot", i === fpIdx);
        kids[i].classList.toggle("done", i < fpIdx);
      }
      fpIdx = (fpIdx + 1) % (STATES.length + 3);
    }, 620);
  }

  /* ─────────── architecture scroll link ─────────── */
  var aSteps = $$(".arch-steps li[data-astep]"), aNodes = $$(".arch-node[data-step]");
  function paintArch() {
    if (!aSteps.length) return;
    var mid = window.innerHeight * 0.52, best = null, bd = 1e9;
    aSteps.forEach(function (li) {
      var r = li.getBoundingClientRect(), d = Math.abs(r.top + r.height / 2 - mid);
      if (d < bd) { bd = d; best = li; }
    });
    var step = best ? best.dataset.astep : null;
    aSteps.forEach(function (li) { li.classList.toggle("on", li === best); });
    aNodes.forEach(function (g) { g.classList.toggle("on", step !== null && +g.dataset.step <= +step); });
  }
  window.addEventListener("scroll", paintArch, { passive: true });

  /* ─────────── cell pointer glow ─────────── */
  $$(".cell").forEach(function (c) {
    c.addEventListener("pointermove", function (e) {
      var r = c.getBoundingClientRect();
      c.style.setProperty("--mx", ((e.clientX - r.left) / r.width * 100) + "%");
      c.style.setProperty("--my", ((e.clientY - r.top) / r.height * 100) + "%");
    });
  });

  /* ─────────── fleet simulation (shared state) ─────────── */
  var ROOMS = ["客厅主力", "书房书架箱", "卧室床头", "厨房吸顶", "机柜核心", "实验室基准", "影音室前级", "阳台障板",
    "茶室号角", "地下室低音", "阁楼监听", "走廊吸顶", "花园防水", "工作室近场", "琴房立式", "试听室旗舰",
    "主卧落地", "儿童房书架", "餐厅嵌入", "门厅背景", "泳池户外", "车库工作", "客房床头", "画室障板"];
  var ARCHES = ["arm64", "amd64", "armhf", "arm64", "amd64"];
  var BOMS = [["renderer-standard", 4], ["core-server", 1], ["renderer-hq", 1]];
  var DEFAULTS = {
    count: 18, seats: 16, play: 35, hb: 15, speed: 100, off: 45, glow: 60, size: 100, orbit: 50,
    links: 1, rings: 3, mode: "enforce", theme: "amber", playgate: true, grid: false, ghost: false
  };
  var F = {
    p: Object.assign({}, DEFAULTS), nodes: [], jobs: [], ringPlan: [], ringIdx: -1,
    running: false, t: 0, sel: null, packets: [], seq: 0, rejected: 0, delivered: 0, failed: 0
  };

  function makeNode(i) {
    var b = pick(BOMS);
    return {
      id: "n-" + (1000 + i), alias: ROOMS[i % ROOMS.length] + (i >= ROOMS.length ? " " + (Math.floor(i / ROOMS.length) + 1) : ""),
      group: pick(["住宅", "商用", "测试", "影音"]), arch: ARCHES[i % ARCHES.length],
      bom: b[0], ver: b[1], playing: Math.random() * 100 < F.p.play, status: "online",
      cpu: rnd(4, 46), mem: rnd(180, 1900), temp: rnd(38, 62), dac: Math.random() < 0.34,
      seen: performance.now(), phaseLeft: rnd(3000, 18000), targetVer: 0,
      ang: 0, ring: 0, rad: 0, x: 0, y: 0, tx: 0, ty: 0, pulse: Math.random() * 6.28,
      job: null, authorized: false, upd: 0, flash: 0
    };
  }
  function rebuild() {
    var n = F.p.count, old = F.nodes;
    F.nodes = [];
    for (var i = 0; i < n; i++) F.nodes.push(old[i] || makeNode(i));
    F.nodes.forEach(function (nd, i) {
      nd.playing = Math.random() * 100 < F.p.play;
      nd.authorized = i < F.p.seats;
      layout(nd, i, n);
    });
    if (F.sel && F.nodes.indexOf(F.sel) < 0) { F.sel = null; closeDetail(); }
    buildRings();
    paintStats();
  }
  function layout(nd, i, n) {
    var ringsN = Math.max(1, Math.ceil(n / 12));
    var ring = Math.floor(i / Math.ceil(n / ringsN));
    var per = Math.ceil(n / ringsN);
    var idx = i % per;
    nd.ring = ring;
    nd.ang = (idx / per) * Math.PI * 2 + ring * 0.42;
    nd.rad = 0.30 + (ring / Math.max(1, ringsN)) * (0.30 + F.p.orbit / 100 * 0.30);
  }
  function buildRings() {
    var k = clamp(F.p.rings, 1, 5), n = F.nodes.length;
    var base = Math.floor(100 / k), plan = [], acc = 0;
    for (var i = 0; i < k; i++) {
      var pct = i === k - 1 ? 100 - acc : base; acc += pct;
      plan.push({ name: ["canary", "early", "half", "broad", "all"][i] || ("ring" + i), percent: pct, nodes: [], done: 0, rejected: 0, failed: 0, settled: 0, status: "pending" });
    }
    var cursor = 0;
    plan.forEach(function (r) {
      var take = Math.max(1, Math.round(n * r.percent / 100));
      r.nodes = F.nodes.slice(cursor, cursor + take); cursor += take;
    });
    if (cursor < n) plan[plan.length - 1].nodes = plan[plan.length - 1].nodes.concat(F.nodes.slice(cursor));
    F.ringPlan = plan;
    if (F.ringIdx >= plan.length) F.ringIdx = -1;
    paintRings();
  }

  /* ── demo controls ── */
  function bindRange(id, out, key, fmt, after) {
    var el = $("#" + id), o = $("#" + out);
    if (!el) return;
    function paint() {
      var v = +el.value, min = +el.min, max = +el.max;
      el.style.setProperty("--fill", ((v - min) / (max - min) * 100) + "%");
      if (o) o.textContent = fmt(v);
    }
    el.addEventListener("input", function () {
      F.p[key] = +el.value; paint(); if (after) after();
    });
    el._paint = paint; paint();
  }
  bindRange("c-count", "o-count", "count", function (v) { return v; }, rebuild);
  bindRange("c-seats", "o-seats", "seats", function (v) { return v; }, function () {
    F.nodes.forEach(function (nd, i) { nd.authorized = i < F.p.seats; });
    paintStats();
    if (F.p.count > F.p.seats) toast("超出授权席位 " + (F.p.count - F.p.seats) + " 台 · enforce 下将被门禁拦截", "warn");
  });
  bindRange("c-play", "o-play", "play", function (v) { return v + "%"; }, function () {
    F.nodes.forEach(function (n) { n.playing = Math.random() * 100 < F.p.play; }); paintStats();
  });
  bindRange("c-hb", "o-hb", "hb", function (v) { return v + "s"; });
  bindRange("c-speed", "o-speed", "speed", function (v) { return (v / 100).toFixed(1) + "×"; });
  bindRange("c-off", "o-off", "off", function (v) { return v + "s"; });
  bindRange("c-glow", "o-glow", "glow", function (v) { return v; });
  bindRange("c-size", "o-size", "size", function (v) { return (v / 100).toFixed(1) + "×"; });
  bindRange("c-orbit", "o-orbit", "orbit", function (v) { return v; }, function () { F.nodes.forEach(function (n, i) { layout(n, i, F.nodes.length); }); });
  bindRange("c-links", "o-links", "links", function (v) { return v ? "开" : "关"; });

  var oRings = $("#o-rings");
  function paintRingCtl() {
    if (oRings) oRings.textContent = F.p.rings;
    var d = $("#ring-dec"), u = $("#ring-inc");
    if (d) d.disabled = F.p.rings <= 1;
    if (u) u.disabled = F.p.rings >= 5;
  }
  function setRings(v) {
    F.p.rings = clamp(v, 1, 5); paintRingCtl();
    if (F.running) { toast("灰度进行中，环数已锁定", "info"); return; }
    F.ringIdx = -1; buildRings();
  }
  var rd = $("#ring-dec"), ru = $("#ring-inc");
  if (rd) rd.addEventListener("click", function () { setRings(F.p.rings - 1); Audio2.blip(320, .1, "triangle", .1); });
  if (ru) ru.addEventListener("click", function () { setRings(F.p.rings + 1); Audio2.blip(480, .1, "triangle", .1); });
  paintRingCtl();

  var cMode = $("#c-mode"), cTheme = $("#c-theme");
  if (cMode) cMode.addEventListener("change", function () { F.p.mode = cMode.value; paintStats(); });
  if (cTheme) cTheme.addEventListener("change", function () {
    F.p.theme = cTheme.value;
    var h = HUES[F.p.theme];
    document.documentElement.style.setProperty("--amber", h.key);
    document.documentElement.style.setProperty("--gold", h.lit);
    document.documentElement.style.setProperty("--warm-3", h.sig);
    toast("信号配色已切换：" + cTheme.selectedOptions[0].textContent, "ok");
  });
  ["playgate", "grid", "ghost"].forEach(function (k) {
    var el = $("#c-" + k);
    if (el) el.addEventListener("change", function () { F.p[k] = el.checked; paintStats(); });
  });

  /* ── rollout ── */
  function startRollout() {
    if (F.running) { toast("灰度已在进行中", "info"); return; }
    F.running = true; F.ringIdx = 0; F.jobs = []; F.delivered = 0; F.rejected = 0; F.failed = 0;
    F.ringPlan.forEach(function (r) { r.done = 0; r.rejected = 0; r.failed = 0; r.settled = 0; r.status = "pending"; });
    F.ringPlan[0].status = "running";
    enqueueRing(0);
    paintRings(); paintStats();
    var run = $("#d-run"); if (run) { run.textContent = "灰度进行中…"; run.disabled = true; }
    Audio2.blip(520, .22, "triangle", .13);
    toast("灰度下发已启动 · " + F.ringPlan.length + " 环 · " + F.p.mode, "ok");
  }
  function settle(r, kind) {
    if (!r) return;
    r.settled++;
    if (kind === "done") r.done++; else if (kind === "rejected") r.rejected++; else r.failed++;
    if (r.settled >= r.nodes.length) finishRing();
  }
  function enqueueRing(ri) {
    var r = F.ringPlan[ri]; if (!r) return;
    r.nodes.forEach(function (nd) {
      if (F.p.mode === "enforce" && !nd.authorized) {
        F.rejected++;
        pushJob(nd, "rejected", "授权门禁拦截");
        settle(r, "rejected");
        return;
      }
      nd.upd = 1; nd.targetVer = nd.ver + 1;
      pushJob(nd, "precheck", null);
    });
    if (!r.nodes.length) finishRing();
  }
  function pushJob(nd, state, note) {
    var j = { id: "job-" + (++F.seq), node: nd, state: state, note: note, p: 0, si: STATES.indexOf(state), t: 0 };
    if (state === "rejected" || state === "failed_rolledback") { j.p = 100; j.si = -1; }
    nd.job = j; F.jobs.unshift(j);
    for (var i = F.jobs.length - 1; i >= 0 && F.jobs.length > 40; i--) {
      var s = F.jobs[i].state;
      if (s === "done" || s === "rejected" || s === "failed_rolledback") F.jobs.splice(i, 1);
    }
    paintJobs();
  }
  function advanceJob(j, dt) {
    var st = j.state;
    if (st === "done" || st === "rejected" || st === "failed_rolledback") return;
    var r = F.ringPlan[F.ringIdx];
    if (j.node.status === "offline") {
      j.state = "failed_rolledback"; j.p = 100; j.si = -1; j.note = "节点离线 · 已回滚";
      j.node.upd = 0; j.node.flash = 1; F.failed++;
      settle(r, "failed");
      Audio2.blip(190, .2, "sawtooth", .07);
      paintJobs(); paintRings(); paintStats();
      return;
    }
    j.t += dt;
    var dwell = 420 / (F.p.speed / 100);
    if (j.state === "waiting_idle") {
      if (!F.p.playgate || !j.node.playing) {
        j.state = "downloading"; j.si = STATES.indexOf(j.state); j.t = 0; j.note = null;
      } else {
        j.p = 22; j.note = "等待曲目结束 " + Math.max(1, Math.ceil(j.node.phaseLeft / 1000)) + "s";
        return;
      }
    }
    if (j.t < dwell) { j.p = clamp((j.si + j.t / dwell) / (STATES.length - 1) * 100, 0, 99); return; }
    j.t = 0; j.si++;
    if (j.si >= STATES.length - 1) {
      j.state = "done"; j.p = 100; j.note = null;
      j.node.upd = 0; j.node.flash = 1; j.node.ver = j.node.targetVer || j.node.ver;
      F.delivered++;
      settle(r, "done");
      Audio2.blip(880, .09, "sine", .07);
      paintJobs(); paintRings(); paintStats();
      return;
    }
    j.state = STATES[j.si];
    paintJobs();
  }
  function finishRing() {
    var r = F.ringPlan[F.ringIdx]; if (!r) return;
    r.status = "done";
    if (F.ringIdx + 1 < F.ringPlan.length) {
      F.ringIdx++;
      F.ringPlan[F.ringIdx].status = "running";
      enqueueRing(F.ringIdx);
      Audio2.blip(660, .18, "triangle", .11);
      toast("环 " + r.name + " 完成，推进到 " + F.ringPlan[F.ringIdx].name, "ok");
    } else {
      F.running = false;
      var run = $("#d-run"); if (run) { run.textContent = "开始灰度下发"; run.disabled = false; }
      toast("灰度完成 · 成功 " + F.delivered + " · 拦截 " + F.rejected + " · 回滚 " + F.failed, "ok");
      Audio2.blip(1046, .3, "sine", .12);
    }
    paintRings(); paintStats();
  }

  /* ── demo buttons ── */
  var dRun = $("#d-run");
  if (dRun) dRun.addEventListener("click", startRollout);
  var dRand = $("#d-rand");
  if (dRand) dRand.addEventListener("click", function () {
    F.p.count = 4 + ((Math.random() * 45) | 0);
    F.p.seats = 4 + ((Math.random() * 45) | 0);
    F.p.play = (Math.random() * 101) | 0;
    F.p.hb = 5 + ((Math.random() * 56) | 0);
    F.p.speed = 20 + ((Math.random() * 281) | 0);
    F.p.off = 15 + ((Math.random() * 34) | 0) * 5;
    F.p.glow = (Math.random() * 101) | 0;
    F.p.size = 60 + ((Math.random() * 25) | 0) * 5;
    F.p.orbit = (Math.random() * 101) | 0;
    F.p.rings = 1 + ((Math.random() * 5) | 0);
    F.p.theme = pick(Object.keys(HUES));
    F.p.grid = Math.random() < .4; F.p.ghost = Math.random() < .4;
    syncControls(); rebuild(); paintRingCtl();
    var h = HUES[F.p.theme];
    document.documentElement.style.setProperty("--amber", h.key);
    document.documentElement.style.setProperty("--gold", h.lit);
    document.documentElement.style.setProperty("--warm-3", h.sig);
    Audio2.blip(700, .16, "triangle", .12);
    toast("参数已随机化", "ok");
  });
  var dReset = $("#d-reset");
  if (dReset) dReset.addEventListener("click", function () {
    F.p = Object.assign({}, DEFAULTS); F.running = false; F.ringIdx = -1; F.jobs = []; F.seq = 0;
    F.delivered = 0; F.rejected = 0; F.failed = 0;
    F.nodes = []; F.packets = []; F.sel = null; closeDetail();
    syncControls(); rebuild(); paintRingCtl(); paintJobs();
    var run = $("#d-run"); if (run) { run.textContent = "开始灰度下发"; run.disabled = false; }
    document.documentElement.style.setProperty("--amber", "#f6a821");
    document.documentElement.style.setProperty("--gold", "#ffd479");
    document.documentElement.style.setProperty("--warm-3", "#ff8a3d");
    toast("已恢复默认参数", "info");
  });
  var dExport = $("#d-export");
  if (dExport) dExport.addEventListener("click", function () {
    var data = {
      exported_at: new Date().toISOString(), mode: F.p.mode, play_gate: F.p.playgate,
      heartbeat_seconds: F.p.hb, offline_threshold_seconds: F.p.off, seats: F.p.seats,
      rings: F.ringPlan.map(function (r) { return { name: r.name, percent: r.percent, nodes: r.nodes.length, done: r.done, status: r.status }; }),
      nodes: F.nodes.map(function (n) {
        return { id: n.id, alias: n.alias, arch: n.arch, group: n.group, status: n.status, playing: n.playing, bom_id: n.bom, bom_version: n.ver, authorized: n.authorized, dac_holder: n.dac, cpu: +n.cpu.toFixed(1), mem_mb: Math.round(n.mem), temp_c: +n.temp.toFixed(1) };
      }),
      jobs: F.jobs.slice(0, 20).map(function (j) { return { id: j.id, node: j.node.alias, state: j.state, progress: Math.round(j.p), note: j.note }; })
    };
    var blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
    var a = document.createElement("a");
    a.href = URL.createObjectURL(blob); a.download = "zephyr-fleet-snapshot.json";
    document.body.appendChild(a); a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 400);
    toast("机群快照已导出 JSON", "ok");
  });
  function syncControls() {
    [["c-count", "count"], ["c-seats", "seats"], ["c-play", "play"], ["c-hb", "hb"], ["c-speed", "speed"], ["c-off", "off"],
     ["c-glow", "glow"], ["c-size", "size"], ["c-orbit", "orbit"], ["c-links", "links"]].forEach(function (p) {
      var el = $("#" + p[0]); if (el) { el.value = F.p[p[1]]; if (el._paint) el._paint(); }
    });
    if (cMode) cMode.value = F.p.mode;
    if (cTheme) cTheme.value = F.p.theme;
    ["playgate", "grid", "ghost"].forEach(function (k) { var e = $("#c-" + k); if (e) e.checked = !!F.p[k]; });
  }

  /* ── stats / rings / jobs DOM ── */
  function paintStats() {
    var on = 0, pl = 0, off = 0;
    F.nodes.forEach(function (n) { if (n.status === "offline") off++; else { on++; if (n.playing) pl++; } });
    var set = function (id, v) { var e = $("#" + id); if (e) e.textContent = v; };
    set("ds-online", on); set("ds-play", pl); set("ds-off", off);
    set("ds-ring", F.ringIdx >= 0 && F.ringPlan[F.ringIdx] ? F.ringPlan[F.ringIdx].name : "—");
    var used = F.nodes.filter(function (n) { return n.authorized; }).length;
    var seatEl = $("#ds-seats");
    if (seatEl) {
      seatEl.textContent = used + "/" + F.p.seats;
      seatEl.className = F.nodes.length > F.p.seats ? "over" : "";
      seatEl.title = F.nodes.length > F.p.seats
        ? "超出授权席位 " + (F.nodes.length - F.p.seats) + " 台 · enforce 模式下将被门禁拦截"
        : "已授权 " + used + " 台 / 许可席位 " + F.p.seats;
    }
    var m = $("#ds-mode"); if (m) { m.textContent = F.p.mode; m.className = "pill " + (F.p.mode === "enforce" ? "" : "red"); }
    var g = $("#ds-gate"); if (g) { g.textContent = F.p.playgate ? "门控开" : "门控关"; g.className = "pill " + (F.p.playgate ? "cyan" : "red"); }
    set("hp-online", on + " / " + F.nodes.length); set("hp-play", pl); set("hp-jobs", F.jobs.filter(function (j) { return j.state !== "done" && j.state !== "rejected"; }).length);
    paintHeroRows();
  }
  function paintRings() {
    var box = $("#ds-rings"); if (!box) return;
    box.innerHTML = F.ringPlan.map(function (r, i) {
      var pct = r.nodes.length ? Math.round(r.settled / r.nodes.length * 100) : 0;
      var cls = r.status === "done" ? "done" : (i === F.ringIdx && F.running ? "active" : "");
      var tail = r.done + "/" + r.nodes.length + " 终端";
      if (r.rejected) tail += " · 拦截 " + r.rejected;
      if (r.failed) tail += " · 回滚 " + r.failed;
      return '<div class="ring ' + cls + '"><b>' + r.name + " · " + r.percent + '%</b><i style="--p:' + pct + '%"></i><span>' + tail + "</span></div>";
    }).join("");
  }
  var SC = { precheck: ["预检", "cyan"], resolve_deps: ["解析依赖", "cyan"], waiting_idle: ["等待空闲", "amber"], downloading: ["下载", "cyan"], verifying: ["验签", "violet"], installing: ["安装", "amber"], rendering_config: ["渲染配置", "cyan"], enabling_units: ["启用单元", "cyan"], validating: ["验证", "violet"], done: ["完成", "green"], rejected: ["已拦截", "red"], failed_rolledback: ["已回滚", "red"] };
  var PC = { cyan: "rgba(52,211,235,.14);color:#34d3eb", amber: "rgba(246,168,33,.15);color:#f6a821", violet: "rgba(155,140,255,.15);color:#9b8cff", green: "rgba(70,224,138,.14);color:#46e08a", red: "rgba(255,107,107,.15);color:#ff6b6b" };
  function paintJobs() {
    var box = $("#dj-list"), cnt = $("#dj-count"); if (!box) return;
    if (cnt) cnt.textContent = F.jobs.length + " 个任务";
    if (!F.jobs.length) { box.innerHTML = '<div class="dj-empty">暂无任务 · 按「开始灰度下发」运行完整状态机</div>'; return; }
    box.innerHTML = F.jobs.slice(0, 14).map(function (j) {
      var s = SC[j.state] || [j.state, "cyan"];
      return '<div class="dj"><span class="dj-n">' + esc(j.node.alias) + "<em>" + j.id + "</em></span>" +
        '<span class="dj-s" style="background:' + PC[s[1]] + '">' + s[0] + (j.note ? " · " + esc(j.note) : "") + "</span>" +
        '<span class="dj-bar"><i style="--p:' + Math.round(j.p) + '%"></i></span></div>';
    }).join("");
  }
  function esc(s) { return String(s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; }); }
  function paintHeroRows() {
    var box = $("#hp-rows"); if (!box) return;
    box.innerHTML = F.nodes.slice(0, 6).map(function (n) {
      var col = n.status === "offline" ? C.off : n.playing ? C.play : C.online;
      var st = n.status === "offline" ? ["离线", "rgba(255,107,107,.14);color:#ff6b6b"] : n.playing ? ["播放中", "rgba(246,168,33,.15);color:#f6a821"] : ["空闲", "rgba(70,224,138,.13);color:#46e08a"];
      return '<div class="hp-row"><i style="background:' + col + ';box-shadow:0 0 8px ' + col + '"></i>' +
        '<span class="n">' + esc(n.alias) + '</span><span class="s">' + n.arch + '</span>' +
        '<span class="p" style="background:' + st[1] + '">' + st[0] + "</span></div>";
    }).join("");
  }

  /* ── detail panel ── */
  var detail = $("#demo-detail"), ddBody = $("#dd-body"), ddAlias = $("#dd-alias");
  function openDetail(n) {
    F.sel = n; if (!detail) return;
    detail.hidden = false;
    if (ddAlias) ddAlias.textContent = n.alias;
    paintDetail();
    Audio2.blip(600, .08, "sine", .09);
  }
  function paintDetail() {
    var n = F.sel; if (!n || !ddBody) return;
    var st = n.status === "offline" ? "离线" : n.playing ? "播放中" : "空闲";
    ddBody.innerHTML =
      row("节点 ID", n.id) + row("分组 / 架构", n.group + " · " + n.arch) +
      row("状态", st) + row("期望 BOM", n.bom + "@" + n.ver) +
      row("授权", n.authorized ? "已授权" : "未授权") +
      row("DAC 占用", n.dac ? "持有" : "释放") +
      row("当前任务", n.job ? (SC[n.job.state] || [n.job.state])[0] : "无") +
      meter("CPU", n.cpu, 100, n.cpu.toFixed(0) + "%") +
      meter("内存", n.mem, 2048, Math.round(n.mem) + " MB") +
      meter("温度", n.temp, 85, n.temp.toFixed(1) + " °C");
  }
  function row(k, v) { return '<div class="dd-r"><span>' + k + "</span><b>" + esc(v) + "</b></div>"; }
  function meter(k, v, max, label) {
    return '<div class="dd-r"><span>' + k + "</span><b>" + label + '</b></div><div class="dd-meter"><i style="width:' + clamp(v / max * 100, 0, 100) + '%"></i></div>';
  }
  function closeDetail() { if (detail) detail.hidden = true; F.sel = null; }
  var ddClose = $("#dd-close"); if (ddClose) ddClose.addEventListener("click", closeDetail);

  /* ─────────── canvases ─────────── */
  function fitCanvas(cv) {
    var r = cv.getBoundingClientRect();
    var dpr = Math.min(window.devicePixelRatio || 1, 2);
    var w = Math.max(1, Math.round(r.width * dpr)), h = Math.max(1, Math.round(r.height * dpr));
    if (cv.width !== w || cv.height !== h) { cv.width = w; cv.height = h; }
    return { w: r.width, h: r.height, dpr: dpr };
  }

  /* hero canvas */
  var hcv = $("#hero-cv"), hctx = hcv ? hcv.getContext("2d") : null;
  var hVisible = true, parts = [];
  function seedParts(w, h) {
    parts = [];
    var n = clamp(Math.round(w * h / 26000), 24, 78);
    for (var i = 0; i < n; i++) parts.push({ x: Math.random() * w, y: Math.random() * h, vx: rnd(-.13, .13), vy: rnd(-.1, .1), r: rnd(.7, 2.1), a: rnd(.16, .55) });
  }
  if (hcv && "IntersectionObserver" in window) {
    new IntersectionObserver(function (e) { hVisible = e[0].isIntersecting; }, { threshold: 0 }).observe(hcv);
  }
  function drawHero(t, dt) {
    if (!hctx || !hVisible) return;
    var s = fitCanvas(hcv), w = s.w, h = s.h;
    hctx.setTransform(s.dpr, 0, 0, s.dpr, 0, 0);
    hctx.clearRect(0, 0, w, h);
    if (!parts.length || Math.abs(parts._w - w) > 40) { seedParts(w, h); parts._w = w; }
    var hue = HUES[F.p.theme], gl = F.p.glow / 100;
    var spec = Audio2.spectrum();
    var bars = 72, bw = w / bars;
    for (var i = 0; i < bars; i++) {
      var v;
      if (spec) v = spec[Math.floor(i / bars * spec.length)] / 255;
      else v = (Math.sin(t * 0.0011 + i * 0.34) * 0.5 + 0.5) * (Math.sin(t * 0.0004 + i * 0.09) * 0.35 + 0.5);
      v = Math.pow(v, 1.5);
      var bh = 8 + v * h * 0.30;
      var g = hctx.createLinearGradient(0, h - bh, 0, h);
      g.addColorStop(0, hue.lit); g.addColorStop(1, "rgba(246,168,33,0.03)");
      hctx.globalAlpha = 0.13 + gl * 0.3;
      hctx.fillStyle = g;
      hctx.fillRect(i * bw + bw * 0.22, h - bh, bw * 0.56, bh);
    }
    hctx.globalAlpha = 1;
    hctx.lineWidth = 1.5;
    for (var L = 0; L < 3; L++) {
      hctx.beginPath();
      var amp = h * (0.045 + L * 0.022) * (0.5 + gl), yb = h * (0.40 + L * 0.075);
      for (var x = 0; x <= w; x += 6) {
        var y = yb + Math.sin(x * 0.006 + t * 0.0009 + L * 1.7) * amp * Math.sin(x * 0.0013 + t * 0.0004);
        if (x === 0) hctx.moveTo(x, y); else hctx.lineTo(x, y);
      }
      var lg = hctx.createLinearGradient(0, 0, w, 0);
      lg.addColorStop(0, "rgba(52,211,235,0)");
      lg.addColorStop(0.35, L === 0 ? hue.key : L === 1 ? "#34d3eb" : "#ff5db1");
      lg.addColorStop(0.7, L === 0 ? "#34d3eb" : hue.lit);
      lg.addColorStop(1, "rgba(155,140,255,0)");
      hctx.strokeStyle = lg; hctx.globalAlpha = 0.16 + gl * 0.24 - L * 0.04;
      hctx.stroke();
    }
    hctx.globalAlpha = 1;
    for (var p = 0; p < parts.length; p++) {
      var q = parts[p];
      q.x += q.vx * dt * 0.06; q.y += q.vy * dt * 0.06;
      if (q.x < -10) q.x = w + 10; if (q.x > w + 10) q.x = -10;
      if (q.y < -10) q.y = h + 10; if (q.y > h + 10) q.y = -10;
      hctx.beginPath(); hctx.arc(q.x, q.y, q.r, 0, 6.2832);
      hctx.fillStyle = hue.lit; hctx.globalAlpha = q.a * (0.35 + gl * 0.6); hctx.fill();
    }
    hctx.globalAlpha = 1;
    var cx = w * 0.72, cy = h * 0.34, rr = Math.min(w, h) * 0.20;
    var rg = hctx.createRadialGradient(cx, cy, 0, cx, cy, rr);
    rg.addColorStop(0, "rgba(246,168,33," + (0.10 + gl * 0.13) + ")");
    rg.addColorStop(1, "rgba(246,168,33,0)");
    hctx.fillStyle = rg; hctx.beginPath(); hctx.arc(cx, cy, rr, 0, 6.2832); hctx.fill();
  }

  /* hero mini spectrum */
  var hpSpec = $("#hp-spec"), hpBars = [];
  if (hpSpec) { for (var i = 0; i < 40; i++) { var b = document.createElement("i"); hpSpec.appendChild(b); hpBars.push(b); } }

  /* fleet canvas */
  var fcv = $("#fleet-cv"), fctx = fcv ? fcv.getContext("2d") : null;
  var fVisible = true, fW = 0, fH = 0, hover = null;
  if (fcv && "IntersectionObserver" in window) {
    new IntersectionObserver(function (e) { fVisible = e[0].isIntersecting; }, { threshold: 0 }).observe(fcv);
  }
  function nodeAt(mx, my) {
    for (var i = F.nodes.length - 1; i >= 0; i--) {
      var n = F.nodes[i], r = 9 * (F.p.size / 100) + 5;
      if ((mx - n.x) * (mx - n.x) + (my - n.y) * (my - n.y) < r * r) return n;
    }
    return null;
  }
  if (fcv) {
    fcv.addEventListener("pointermove", function (e) {
      var r = fcv.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top;
      hover = nodeAt(mx, my);
      fcv.style.cursor = hover ? "pointer" : "crosshair";
      var tip = $("#ds-tip");
      if (tip) {
        if (hover) {
          tip.hidden = false;
          tip.innerHTML = "<b>" + esc(hover.alias) + "</b><span>" + hover.arch + " · " + hover.bom + "@" + hover.ver +
            "<br>" + (hover.status === "offline" ? "离线" : hover.playing ? "播放中" : "空闲") + " · CPU " + hover.cpu.toFixed(0) + "%</span>";
          var tw = 210;
          tip.style.left = clamp(mx + 14, 6, Math.max(6, r.width - tw)) + "px";
          tip.style.top = clamp(my - 10, 6, Math.max(6, r.height - 76)) + "px";
        } else tip.hidden = true;
      }
    });
    fcv.addEventListener("pointerleave", function () { hover = null; var t = $("#ds-tip"); if (t) t.hidden = true; });
    fcv.addEventListener("click", function (e) {
      var r = fcv.getBoundingClientRect(), n = nodeAt(e.clientX - r.left, e.clientY - r.top);
      if (n) openDetail(n); else closeDetail();
    });
  }
  function drawFleet(t, dt) {
    if (!fctx || !fVisible) return;
    var s = fitCanvas(fcv), w = s.w, h = s.h; fW = w; fH = h;
    fctx.setTransform(s.dpr, 0, 0, s.dpr, 0, 0);
    fctx.clearRect(0, 0, w, h);
    var cx = w / 2, cy = h / 2, base = Math.min(w, h) / 2;
    var hue = HUES[F.p.theme], gl = F.p.glow / 100, sz = F.p.size / 100;
    if (F.p.grid) {
      fctx.strokeStyle = "rgba(147,162,183,.09)"; fctx.lineWidth = 1;
      for (var g = 1; g <= 4; g++) { fctx.beginPath(); fctx.arc(cx, cy, base * (0.22 * g), 0, 6.2832); fctx.stroke(); }
      for (var a = 0; a < 12; a++) {
        fctx.beginPath(); fctx.moveTo(cx, cy);
        fctx.lineTo(cx + Math.cos(a / 12 * 6.2832) * base * 0.92, cy + Math.sin(a / 12 * 6.2832) * base * 0.92); fctx.stroke();
      }
    }
    var rot = reduced ? 0 : t * 0.00006 * (F.p.speed / 100);
    F.nodes.forEach(function (n) {
      var rr = base * n.rad * 1.62;
      n.tx = cx + Math.cos(n.ang + rot * (1 + n.ring * 0.3)) * rr;
      n.ty = cy + Math.sin(n.ang + rot * (1 + n.ring * 0.3)) * rr * 0.80;
      n.x = lerp(n.x || n.tx, n.tx, 0.14); n.y = lerp(n.y || n.ty, n.ty, 0.14);
    });
    if (F.p.links) {
      F.nodes.forEach(function (n) {
        if (n.status === "offline" && !F.p.ghost) return;
        var act = n.upd > 0;
        fctx.beginPath(); fctx.moveTo(cx, cy); fctx.lineTo(n.x, n.y);
        fctx.strokeStyle = act ? "rgba(52,211,235," + (0.16 + gl * 0.3) + ")" : "rgba(147,162,183," + (0.05 + gl * 0.07) + ")";
        fctx.lineWidth = act ? 1.3 : 0.7; fctx.stroke();
      });
    }
    var cr = 20 + Math.sin(t * 0.002) * 2.2;
    var cg = fctx.createRadialGradient(cx, cy, 0, cx, cy, cr * 3.4);
    cg.addColorStop(0, "rgba(246,168,33," + (0.22 + gl * 0.3) + ")"); cg.addColorStop(1, "rgba(246,168,33,0)");
    fctx.fillStyle = cg; fctx.beginPath(); fctx.arc(cx, cy, cr * 3.4, 0, 6.2832); fctx.fill();
    fctx.beginPath();
    for (var k = 0; k < 6; k++) {
      var an = k / 6 * 6.2832 - Math.PI / 2 + rot * 2;
      var px = cx + Math.cos(an) * cr, py = cy + Math.sin(an) * cr;
      if (k === 0) fctx.moveTo(px, py); else fctx.lineTo(px, py);
    }
    fctx.closePath();
    var hg = fctx.createLinearGradient(cx - cr, cy - cr, cx + cr, cy + cr);
    hg.addColorStop(0, hue.lit); hg.addColorStop(1, hue.key);
    fctx.fillStyle = hg; fctx.globalAlpha = .92; fctx.fill(); fctx.globalAlpha = 1;
    fctx.strokeStyle = "rgba(255,255,255,.35)"; fctx.lineWidth = 1; fctx.stroke();
    fctx.fillStyle = "#20160a"; fctx.font = "600 9px " + "ui-monospace,monospace"; fctx.textAlign = "center"; fctx.textBaseline = "middle";
    fctx.fillText("SRV", cx, cy + .5);

    F.nodes.forEach(function (n) {
      var off = n.status === "offline";
      if (off && !F.p.ghost) return;
      var r = 7.2 * sz;
      var col = off ? C.off : n.upd > 0 ? C.upd : n.playing ? hue.key : C.online;
      if (gl > 0.02) {
        var gg = fctx.createRadialGradient(n.x, n.y, 0, n.x, n.y, r * (3 + gl * 3));
        gg.addColorStop(0, hexA(col, 0.20 + gl * 0.34)); gg.addColorStop(1, hexA(col, 0));
        fctx.fillStyle = gg; fctx.beginPath(); fctx.arc(n.x, n.y, r * (3 + gl * 3), 0, 6.2832); fctx.fill();
      }
      if (n.playing && !off) {
        var pr = r + 4 + (Math.sin(t * 0.004 + n.pulse) * 0.5 + 0.5) * 5 * sz;
        fctx.beginPath(); fctx.arc(n.x, n.y, pr, 0, 6.2832);
        fctx.strokeStyle = hexA(hue.key, 0.42); fctx.lineWidth = 1.3; fctx.stroke();
      }
      if (n.upd > 0) {
        fctx.beginPath(); fctx.arc(n.x, n.y, r + 6, t * 0.004, t * 0.004 + 2.1);
        fctx.strokeStyle = C.upd; fctx.lineWidth = 2; fctx.stroke();
      }
      fctx.beginPath(); fctx.arc(n.x, n.y, r, 0, 6.2832);
      if (off && F.p.ghost) { fctx.fillStyle = "rgba(255,107,107,.07)"; fctx.fill(); fctx.setLineDash([3, 3]); fctx.strokeStyle = hexA(C.off, .6); fctx.lineWidth = 1.2; fctx.stroke(); fctx.setLineDash([]); }
      else { fctx.fillStyle = col; fctx.globalAlpha = .93; fctx.fill(); fctx.globalAlpha = 1; fctx.strokeStyle = "rgba(0,0,0,.35)"; fctx.lineWidth = 1; fctx.stroke(); }
      if (n.dac && !off) { fctx.beginPath(); fctx.arc(n.x, n.y, r * 0.4, 0, 6.2832); fctx.fillStyle = "rgba(7,9,13,.82)"; fctx.fill(); }
      if (n.flash > 0) {
        fctx.beginPath(); fctx.arc(n.x, n.y, r + (1 - n.flash) * 22, 0, 6.2832);
        fctx.strokeStyle = hexA(C.online, n.flash * .7); fctx.lineWidth = 2; fctx.stroke();
        n.flash = Math.max(0, n.flash - dt * 0.0016);
      }
      if (n === F.sel || n === hover) {
        fctx.beginPath(); fctx.arc(n.x, n.y, r + 8, 0, 6.2832);
        fctx.strokeStyle = n === F.sel ? C.sel : "rgba(255,255,255,.5)"; fctx.lineWidth = n === F.sel ? 2 : 1.2; fctx.stroke();
      }
    });
    for (var p = F.packets.length - 1; p >= 0; p--) {
      var pk = F.packets[p]; pk.t += dt * 0.0013 * (F.p.speed / 100);
      if (pk.t >= 1) { F.packets.splice(p, 1); continue; }
      var x = lerp(cx, pk.n.x, pk.t), y = lerp(cy, pk.n.y, pk.t);
      fctx.beginPath(); fctx.arc(x, y, 2.6, 0, 6.2832);
      fctx.fillStyle = hue.lit; fctx.shadowColor = hue.key; fctx.shadowBlur = 8 * gl + 3; fctx.fill(); fctx.shadowBlur = 0;
    }
  }
  function hexA(hex, a) {
    var h = hex.replace("#", "");
    var r = parseInt(h.substring(0, 2), 16), g = parseInt(h.substring(2, 4), 16), b = parseInt(h.substring(4, 6), 16);
    return "rgba(" + r + "," + g + "," + b + "," + a + ")";
  }

  /* ─────────── simulation tick ─────────── */
  var hbAcc = 0, telAcc = 0, statAcc = 0, detAcc = 0, pktAcc = 0;
  function tick(t, dt) {
    var sp = F.p.speed / 100;
    hbAcc += dt; telAcc += dt; statAcc += dt; detAcc += dt; pktAcc += dt;
    if (telAcc > 110 / sp) {
      telAcc = 0;
      F.nodes.forEach(function (n) {
        if (n.status === "offline") return;
        n.cpu = clamp(n.cpu + rnd(-4, 4) + (n.playing ? 1.1 : -0.5), 1, 99);
        n.mem = clamp(n.mem + rnd(-24, 26), 120, 2040);
        n.temp = clamp(n.temp + rnd(-.5, .55) + (n.playing ? .07 : -.04), 32, 82);
      });
    }
    if (hbAcc > F.p.hb * 1000 / sp) {
      hbAcc = 0;
      F.nodes.forEach(function (n) {
        if (Math.random() < 0.035) { n.status = "offline"; }
        else { n.status = "online"; n.seen = t; }
      });
    }
    F.nodes.forEach(function (n) {
      if (n.status === "online" && t - n.seen > F.p.off * 1000) n.status = "offline";
      if (n.status !== "offline") {
        n.phaseLeft -= dt * sp;
        if (n.phaseLeft <= 0) {
          n.playing = Math.random() * 100 < F.p.play;
          n.phaseLeft = n.playing ? rnd(5000, 15000) : rnd(5000, 16000);
        }
      }
    });
    if (F.running) {
      F.jobs.slice().forEach(function (j) { advanceJob(j, dt); });
      if (pktAcc > 130 / sp) {
        pktAcc = 0;
        var act = F.nodes.filter(function (n) { return n.upd > 0; });
        if (act.length) F.packets.push({ n: pick(act), t: 0 });
        if (F.packets.length > 60) F.packets.shift();
      }
    }
    if (statAcc > 420) { statAcc = 0; paintStats(); paintRings(); }
    if (detAcc > 380) { detAcc = 0; if (F.sel) paintDetail(); }
    if (hpBars.length) {
      var spec = Audio2.spectrum();
      for (var i = 0; i < hpBars.length; i++) {
        var v = spec ? spec[Math.floor(i / hpBars.length * spec.length)] / 255
          : (Math.sin(t * 0.0016 + i * 0.42) * .5 + .5) * (Math.sin(t * 0.0006 + i * .13) * .4 + .55);
        hpBars[i].style.height = (10 + Math.pow(v, 1.4) * 88) + "%";
      }
    }
  }

  /* ─────────── single rAF ─────────── */
  var last = 0, raf = 0;
  function frame(ts) {
    raf = requestAnimationFrame(frame);
    if (!last) last = ts;
    var dt = Math.min(64, ts - last); last = ts;
    if (document.hidden) return;
    tick(ts, dt);
    drawHero(ts, dt);
    drawFleet(ts, dt);
  }
  raf = requestAnimationFrame(frame);
  document.addEventListener("visibilitychange", function () { last = 0; });

  /* ─────────── security gate demos ─────────── */
  function lines(box, arr) {
    if (!box) return;
    box.innerHTML = arr.map(function (l) { return '<span class="ln ' + (l[0] || "") + '">' + esc(l[1]) + "</span>"; }).join("");
  }
  var gEnf = $("#gate-enforce"), gSig = $("#gate-signed"), gTru = $("#gate-trusted");
  function gatePill() {
    var p = $("#gate-mode-pill"); if (!p) return;
    p.textContent = gEnf && gEnf.checked ? "enforce" : "dev";
    p.className = "pill " + (gEnf && gEnf.checked ? "" : "red");
  }
  [gEnf, gSig, gTru].forEach(function (e) { if (e) e.addEventListener("change", gatePill); });
  gatePill();
  var gSub = $("#gate-submit");
  if (gSub) gSub.addEventListener("click", function () {
    var out = $("#gate-out"), enf = gEnf.checked, sig = gSig.checked, tru = gTru.checked;
    var L = [["in", "$ hifihub repo ingest roonbridge_2.0.14_arm64.deb"]];
    if (!enf) {
      L.push(["wn", "signature_mode=dev · 跳过验签（仅限本地联调）"]);
      L.push(["ok", "HTTP 201 · 制品已入库"]);
      L.push(["wn", "生产环境必须切回 enforce"]);
      Audio2.blip(300, .18, "sawtooth", .1);
    } else if (!sig) {
      L.push(["no", "ERROR missing_signature: 未找到分离签名 .asc"]);
      L.push(["no", "HTTP 422 · 拒绝入库（NFR-S2）"]);
      Audio2.blip(150, .26, "sawtooth", .13);
    } else if (!tru) {
      L.push(["wn", "gpg: 签名存在，但签发者不在信任根中"]);
      L.push(["no", "ERROR untrusted_signer: 未知签名子密钥"]);
      L.push(["no", "HTTP 422 · 拒绝入库"]);
      Audio2.blip(150, .26, "sawtooth", .13);
    } else {
      L.push(["ok", "gpg: Good signature · 签名子密钥已在信任根"]);
      L.push(["ok", "sha256: 校验和匹配"]);
      L.push(["ok", "HTTP 201 · 制品已入库，可用于 BOM 绑定"]);
      Audio2.blip(760, .14, "sine", .11);
    }
    lines(out, L);
  });
  var gPlay = $("#gate-playing"), gPG = $("#gate-playgate"), gAuth = $("#gate-authorized");
  function playPill() {
    var p = $("#gate-play-pill"); if (!p) return;
    var playing = gPlay && gPlay.checked;
    p.textContent = playing ? "播放中" : "空闲";
    p.className = "pill " + (playing ? "" : "cyan");
  }
  if (gPlay) gPlay.addEventListener("change", playPill);
  playPill();
  var gDel = $("#gate-deliver");
  if (gDel) gDel.addEventListener("click", function () {
    var out = $("#gate-out2"), playing = gPlay.checked, pg = gPG.checked, auth = gAuth.checked;
    var L = [["in", "$ POST /api/v1/jobs  {node, bom_version}"]];
    if (!auth) {
      L.push(["no", "ERROR unauthorized_node: 终端未授权或席位不足"]);
      L.push(["no", "HTTP 403 · 下发前授权门禁拦截"]);
      Audio2.blip(150, .26, "sawtooth", .13);
    } else if (playing && pg) {
      L.push(["wn", "node.playing=true · play_protect=true"]);
      L.push(["wn", "任务进入 waiting_idle，等待终端空闲"]);
      L.push(["no", "系统升级 / 停服务 / 关机 → HTTP 409"]);
      L.push(["ok", "播放不被中断（FR-3.10 双侧执行）"]);
      Audio2.blip(420, .2, "triangle", .11);
    } else {
      if (playing && !pg) L.push(["wn", "播放门控已关闭 · 将中断播放（不建议）"]);
      L.push(["ok", "precheck → resolve_deps → downloading"]);
      L.push(["ok", "verifying: Good signature"]);
      L.push(["ok", "installing → rendering_config → enabling_units"]);
      L.push(["ok", "validating → done · HTTP 201"]);
      Audio2.blip(880, .16, "sine", .11);
    }
    lines(out, L);
  });

  /* ─────────── copy + arch tabs ─────────── */
  $$(".code[data-copy]").forEach(function (box) {
    var btn = $(".copy", box), pre = $("pre", box);
    if (!btn || !pre) return;
    btn.addEventListener("click", function () {
      var txt = pre.textContent;
      var done = function () {
        btn.textContent = "已复制"; btn.classList.add("ok");
        toast("命令已复制到剪贴板", "ok");
        setTimeout(function () { btn.textContent = "复制"; btn.classList.remove("ok"); }, 1900);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(txt).then(done).catch(function () { fallback(txt, done); });
      } else fallback(txt, done);
    });
  });
  function fallback(txt, done) {
    var ta = document.createElement("textarea");
    ta.value = txt; ta.setAttribute("readonly", ""); ta.style.cssText = "position:fixed;left:-9999px;top:0";
    document.body.appendChild(ta); ta.select();
    try { document.execCommand("copy"); done(); } catch (e) { toast("复制失败，请手动选择命令", "err"); }
    ta.remove();
  }
  var DEBS = {
    amd64: ["hifihub-agent_0.3.0_amd64.deb", "x86_64 终端 Agent · 4.0 MiB"],
    arm64: ["hifihub-agent_0.3.0_arm64.deb", "树莓派 4/5 · 64 位 · 3.6 MiB"],
    armhf: ["hifihub-agent_0.3.0_armhf.deb", "树莓派 2/3 · 32 位 · 3.9 MiB"],
    android: ["zephyr-terminal-0.3.0.apk", "安卓终端 · Android 8.0+ · arm64 / armv7"]
  };
  var DL_NOTES = {
    android: "安卓安装：下载 APK → 允许「安装未知应用」→ 安装并启动「Zephyr 终端」→ 填服务端地址与一次性令牌（控制台「添加终端」向导架构选 Android）。",
    linux: "离线安装：<code>sudo dpkg -i agent/hifihub-agent_0.3.0_&lt;arch&gt;.deb &amp;&amp; sudo apt-get -f install -y</code>"
  };
  $$("#arch-tabs button").forEach(function (b) {
    b.addEventListener("click", function () {
      $$("#arch-tabs button").forEach(function (x) { x.classList.remove("on"); x.setAttribute("aria-selected", "false"); });
      b.classList.add("on"); b.setAttribute("aria-selected", "true");
      var a = b.dataset.arch, d = DEBS[a], link = $("#dl-deb");
      if (link) link.href = "https://github.com/Eyebright/zephyr-music-server/releases/download/v0.3.0/" + d[0];
      var n = $("#dl-deb-name"), m = $("#dl-deb-meta");
      if (n) n.textContent = d[0]; if (m) m.textContent = d[1];
      var note = $("#dl-note"); if (note) note.innerHTML = (a === "android" ? DL_NOTES.android : DL_NOTES.linux);
      Audio2.blip(560, .08, "sine", .08);
    });
  });

  /* ─────────── boot ─────────── */
  rebuild(); syncControls(); paintJobs(); paintStats(); paintRings(); paintArch(); onScroll();
  window.addEventListener("resize", function () { parts = []; });
})();
