"use strict";

(() => {
  let epoch = 0, active = null, writing = false;
  const noSources = "没有适用于该终端发行版的第三方 APT 源（发行版自带软件包无需额外源）";
  const list = v => Array.isArray(v) ? v : [];
  const text = v => typeof v === "string" ? v : "";
  const message = e => e?.message || "读取失败";
  const stable = v => JSON.stringify(v, function (key, value) {
    return value && typeof value === "object" && !Array.isArray(value)
      ? Object.keys(value).sort().reduce((out, k) => { out[k] = value[k]; return out; }, {}) : value;
  });
  const here = c => active === c && c.epoch === epoch && !c.closed;
  const note = (s, kind = "info") => `<div class="banner ${kind}" role="status">${icon(kind === "err" || kind === "warn" ? "alert" : "info", 15)}<span>${esc(s)}</span></div>`;
  const packages = v => [...new Set([v.apt_package, ...list(v.deps_pkgs)].filter(Boolean))];
  const packageName = s => text(s).replace(/\.service$/, "");
  const aptOS = s => /\b(debian|raspbian|ubuntu|linuxmint|linux mint|dietpi|moode|buster|bullseye|bookworm|trixie|focal|jammy|noble)\b/i.test(s);
  const nonLinux = s => /(?:^|[^a-z])(windows|android|darwin|macos|mac os|freebsd|openbsd)(?:$|[^a-z])/i.test(s);
  const supportsDistro = (v, os) => list(v.distro_support).some(d => {
    const term = text(d).trim().toLowerCase();
    return term && (" " + os.toLowerCase().replace(/[^a-z0-9.+-]+/g, " ") + " ").includes(" " + term + " ");
  });
  const aptLabel = a => `${a.app.name || a.app.app_id} · 包 ${a.version.apt_package || "未声明"} · 依赖 ${list(a.version.deps_pkgs).join("、") || "无额外依赖"} · 来源 APT 发行版签名仓库${a.app.upstream ? " / 上游 " + a.app.upstream : ""} · 发行版候选（精确版本由终端解析）`;
  const bomLabel = b => `${b.bom_id}@${b.version} · ${list(b.apps).map(a => `${a.app_id}@${a.version || a.channel || "未固定版本"}`).join("、") || "无应用"}`;

  async function read(path, array = false) {
    try {
      const value = await api("GET", path);
      if (array && !Array.isArray(value)) throw new Error("响应不是列表");
      return { value };
    } catch (e) { return { error: message(e) }; }
  }
  async function readBase(ids) {
    const [nodes, apt, boms, targets] = await Promise.all([
      read("/nodes", true), read("/apt-apps", true), read("/boms?status=published", true),
      Promise.all(ids.map(async id => {
        const [apps, status] = await Promise.all([
          read(`/nodes/${enc(id)}/apps`, true), read(`/nodes/${enc(id)}/local/api/status`)
        ]);
        return { id, apps, status };
      }))
    ]);
    const options = [];
    for (const a of list(apt.value)) {
      if (!a?.app?.app_id || !a?.version) continue;
      options.push({ key: stable(["apt", a.app.app_id, a.version.id || a.version.version, a.version.arch]), kind: "apt", data: a, label: aptLabel(a) });
    }
    for (const b of list(boms.value)) {
      if (b?.status !== "published" || !b.bom_id || !Number.isInteger(b.version) || b.version < 1) continue;
      options.push({ key: stable(["bom", b.bom_id, b.version]), kind: "bom", data: b, label: bomLabel(b) });
    }
    return { nodes, apt, boms, targets, options };
  }
  async function metadata(choice) {
    if (choice?.kind !== "bom") return {};
    const b = choice.data;
    const [latest, versions] = await Promise.all([
      read(`/boms/${enc(b.bom_id)}`),
      Promise.all([...new Set(list(b.apps).map(a => a.app_id).filter(Boolean))].map(async id =>
        [id, await read(`/apps/${enc(id)}/versions`, true)]))
    ]);
    return { latest, versions: Object.fromEntries(versions) };
  }
  function installed(target, id, v = {}) {
    const names = new Set([id, v.apt_package, ...list(v.units).map(packageName)].filter(Boolean));
    const services = list(target.status.value?.services).filter(s => names.has(s.app_id) || names.has(packageName(s.unit)));
    const inventory = list(target.apps.value).filter(a => names.has(a.app_id) || names.has(a.package) || names.has(packageName(a.unit)));
    const records = [...services.filter(s => s.installed === true), ...inventory];
    const versions = [...new Set(records.map(s => text(s.version)).filter(Boolean))];
    const sources = [...new Set(records.map(s => text(s.source)).filter(Boolean))];
    const known = !target.status.error && Array.isArray(target.status.value?.services) && !target.apps.error;
    return {
      present: records.length > 0, system: records.some(s => s.source === "system"),
      unknownSource: records.some(s => !["system", "server"].includes(s.source)), versions, sources,
      conflict: versions.length > 1 || (inventory.length > 0 && services.some(s => s.installed === false)),
      label: !known ? `${id}：状态未知（不能判定未安装）`
        : records.length ? `${id}：${versions.join(" / ") || "已安装，版本未上报"} · ${sources.join(" / ") || "来源未上报"}`
          : `${id}：${services.some(s => s.installed === false) ? "未安装（本地服务状态）" : "未检测到安装（非完整软件包清单）"}`
    };
  }
  function artifactProblems(v, node, os, kind) {
    const reasons = [];
    if (!v) return ["缺少所选版本与终端架构的制品元数据"];
    if (v.arch !== node.arch && v.arch !== "all") reasons.push(`制品架构 ${v.arch || "未知"} 不适配 ${node.arch || "未知"}`);
    if (v.banned || v.lifecycle === "revoked") reasons.push("版本已禁用或撤销");
    if (list(v.distro_support).length && !supportsDistro(v, os)) reasons.push(`发行版不在制品支持范围：${v.distro_support.join("、")}`);
    if (v.glibc_min) reasons.push(`需要 glibc ≥ ${v.glibc_min}，当前状态 API 未提供 glibc 版本，无法验证`);
    if (kind === "apt") {
      if (v.format !== "apt" || !v.apt_package || packages(v).some(p => !/^[a-z0-9][a-z0-9+.-]*$/.test(p))) reasons.push("APT 包元数据不完整或包名不合法");
    } else {
      if (v.arch === "all" && node.arch !== "all") reasons.push("当前 BOM Agent 仅解析终端精确架构，不能交付 all 架构制品");
      if (v.format === "apt") reasons.push("此 BOM 含 APT 条目；现有 BOM Agent 走归档下载，请改用 APT 服务来源");
      else if (!["deb", "tar.bz2"].includes(v.format)) reasons.push(`不支持的 Linux 交付格式：${v.format || "未知"}`);
      if (v.signed !== true || !v.signature || !v.artifact_path || !v.sha256) reasons.push("签名归档或完整性元数据不完整；不能绕过签名交付");
      if (nonLinux(v.artifact_path || "")) reasons.push("制品路径标识为非 Linux 包");
      if ((v.format === "deb" && !/\.deb$/i.test(v.artifact_path || "")) || (v.format === "tar.bz2" && !/\.tar\.bz2$/i.test(v.artifact_path || ""))) reasons.push("制品格式声明与归档文件名不符，无法确认 Linux 安装包");
      if (v.format === "tar.bz2" && !list(v.distro_support).length) reasons.push("归档未声明 Linux 发行版适配，不能仅凭 CPU 架构判定兼容");
    }
    return reasons;
  }
  function preview(base, choice, meta) {
    return base.targets.map(target => {
      const node = list(base.nodes.value).find(n => n.node_id === target.id) || {};
      const status = target.status.value, identity = status?.identity;
      const os = [node.distro, status?.metrics?.distro].filter(Boolean).join(" / ") || "OS 未知";
      const platformOS = text(status?.metrics?.distro) || text(node.distro);
      const row = { id: target.id, alias: node.alias || target.id, os, arch: node.arch || "未知", current: [], changes: [], targets: [], reasons: [], same: [] };
      if (base.nodes.error) row.reasons.push("终端列表读取失败：" + base.nodes.error);
      else if (!node.node_id) row.reasons.push("所选终端已不存在");
      if (node.status !== "online") row.reasons.push("终端离线或在线状态未知");
      if (node.authorized !== true) row.reasons.push("终端未授权");
      if (target.apps.error) row.reasons.push("已装版本读取失败：" + target.apps.error);
      if (target.status.error) row.reasons.push("本地 API 状态读取失败：" + target.status.error);
      else if (!identity || !Array.isArray(status.services)) row.reasons.push("本地 API 状态响应不完整，不能确认安装状态与能力");
      if (identity?.system_available !== true) row.reasons.push("Linux 系统服务管理不可用或能力未上报");
      if (nonLinux(os) || !aptOS(platformOS)) row.reasons.push("不是已确认的 Debian/Ubuntu 系 Linux 平台；不能仅凭 CPU 架构安装");
      if (!["amd64", "arm64", "armhf", "armv7hf"].includes(node.arch)) row.reasons.push("终端架构不受支持或未上报");
      if (identity?.node_id && identity.node_id !== target.id) row.reasons.push("本地 API 终端身份与目标不一致");
      if ((identity?.arch && identity.arch !== node.arch) || (status?.metrics?.arch && status.metrics.arch !== node.arch)) row.reasons.push("终端与本地 API 架构报告不一致");
      if (typeof node.playing !== "boolean" || typeof identity?.playing !== "boolean") row.reasons.push("播放状态未确认，本轮禁止安装");
      if (node.playing || identity?.playing) row.reasons.push("播放保护：终端正在播放，本轮不安装；停止播放后重新预览");
      if (!choice) {
        row.current = target.status.error || target.apps.error || !Array.isArray(status?.services)
          ? ["状态未知，不能判定未安装"]
          : [...list(target.apps.value).map(a => `${a.app_id}：${a.version || "版本未上报"} · ${a.source || "来源未上报"}`), ...status.services.filter(s => s.installed).map(s => `${s.app_id}：${s.version || "版本未上报"} · ${s.source || "来源未上报"}`)];
        if (!row.current.length) row.current.push("未检测到安装（非完整软件包清单）");
        row.reasons.push("请选择安装来源");
        return row;
      }
      let refs;
      if (choice.kind === "apt") refs = [{ app_id: choice.data.app.app_id, version: choice.data.version.version, artifact: choice.data.version }];
      else {
        const b = choice.data;
        if (meta.latest?.error) row.reasons.push("无法核对最新模板：" + meta.latest.error);
        else if (stable(meta.latest?.value) !== stable(b)) row.reasons.push("最新模板与所选已发布版本不同；后端按最新模板执行，本轮禁止提交");
        refs = list(b.apps).map(a => {
          const versions = list(meta.versions?.[a.app_id]?.value).filter(v => v.app_id === a.app_id && v.version === a.version);
          return { ...a, artifact: versions.find(v => v.arch === node.arch) || versions.find(v => v.arch === "all") };
        });
        if (!refs.length) row.reasons.push("模板没有应用");
      }
      for (const ref of refs) {
        const v = ref.artifact, state = installed(target, ref.app_id, v);
        row.current.push(state.label);
        if (!ref.version) row.reasons.push(`${ref.app_id}：模板未固定精确版本`);
        if (meta.versions?.[ref.app_id]?.error) row.reasons.push(`${ref.app_id}：版本元数据读取失败：${meta.versions[ref.app_id].error}`);
        row.reasons.push(...artifactProblems(v, node, platformOS, choice.kind).map(s => `${ref.app_id}：${s}`));
        const affected = new Set([ref.app_id, ...list(v?.deps_apps), ...list(v?.deps_pkgs), ...list(v?.units).map(packageName)]);
        for (const service of list(status?.services)) if (service.app_id === ref.app_id) list(service.helpers).forEach(id => affected.add(id));
        for (const id of affected) {
          const dep = id === ref.app_id ? state : installed(target, id);
          if (id !== ref.app_id && dep.present) row.current.push(dep.label);
          if (dep.system) row.reasons.push(`${id}：系统自带服务（source system），禁止接管更新；请使用原生终端控制台`);
          if (dep.unknownSource) row.reasons.push(`${id}：已安装但来源未知，不能安全接管`);
          if (dep.conflict) row.reasons.push(`${id}：库存与实时版本/状态不一致，请刷新终端后重试`);
        }
        if (choice.kind === "apt") {
          row.targets.push(`发行版候选（精确版本未知）：${packages(v).join("、")}`);
          if (state.present) row.reasons.push(`${ref.app_id}：已安装；无法确认发行版候选是否更新，不自动重装。请用原生终端控制台检查更新`);
          else row.changes.push(`配置适用的第三方 APT 源（如有），安装 ${packages(v).join("、")}；包脚本可能启停服务及安装/更新依赖`);
        } else {
          row.targets.push(`${ref.app_id}@${ref.version || "未固定"} · ${v?.format || "格式未知"}${v?.deps_pkgs?.length ? " · 依赖 " + v.deps_pkgs.join("、") : ""}`);
          if (state.present && state.versions.length === 1 && state.versions[0] === ref.version) row.same.push(ref.app_id);
          else row.changes.push(`${state.present ? "更新" : "安装"} ${ref.app_id}@${ref.version}，应用模板变量/配置，启用并启动服务${v?.deps_pkgs?.length ? "，安装/更新依赖 " + v.deps_pkgs.join("、") : ""}`);
        }
      }
      if (row.same.length) row.reasons.push(row.same.length === refs.length
        ? "所有目标版本均已安装，无需安装"
        : `模板会重装同版本应用 ${row.same.join("、")}；本轮禁止不必要重装，请选择仅含必要变更的已发布模板`);
      row.reasons = [...new Set(row.reasons)];
      row.current = [...new Set(row.current)];
      return row;
    });
  }
  function fingerprint(base, choice, meta, rows) {
    return stable({ choice, meta, rows, targets: base.targets.map(t => {
      const n = list(base.nodes.value).find(n => n.node_id === t.id) || {}, i = t.status.value?.identity || {};
      return { id: t.id, node: [n.status, n.authorized, n.arch, n.distro, n.playing, n.play_protect],
        identity: [i.node_id, i.arch, i.system_available, i.playing, i.play_protect],
        apps: list(t.apps.value).slice().sort((a, b) => text(a.app_id).localeCompare(text(b.app_id))) };
    }) });
  }
  function apply(c, base, key, meta) {
    c.base = base; c.key = key;
    c.choice = base.options.find(o => o.key === key) || null;
    c.meta = meta; c.rows = preview(base, c.choice, meta);
    c.fingerprint = fingerprint(base, c.choice, meta, c.rows);
  }
  const valid = c => !!c.choice && c.rows.length === c.ids.length && c.rows.length > 0 && c.rows.every(r => !r.reasons.length && r.changes.length);
  function render(c) {
    if (!here(c)) return;
    const options = c.base?.options || [], disabled = c.busy || c.loading || c.finished;
    let body = note(`本轮目标固定为 ${c.ids.length} 台，不会自动添加或跳过终端。目标不合适时请关闭窗口，回列表重新选择。`);
    if (c.loading && !c.base) body += note("正在只读加载终端、安装来源与本地服务状态…");
    if (c.error) body += note(c.error, "err");
    for (const [name, result] of [["APT 服务", c.base?.apt], ["已发布模板", c.base?.boms]]) if (result?.error) body += note(`${name}读取失败：${result.error}`, "warn");
    body += `<div class="field"><label for="ti-source">统一安装来源</label><select class="inp" id="ti-source" data-change="ti-source" ${disabled ? "disabled" : ""}><option value="">请选择 APT 服务或已发布部署模板</option>${["apt", "bom"].map(kind => `<optgroup label="${kind === "apt" ? "APT 服务" : "已发布部署模板（BOM）"}">${options.filter(o => o.kind === kind).map(o => `<option value="${esc(o.key)}" ${o.key === c.key ? "selected" : ""}>${esc(o.label)}</option>`).join("")}</optgroup>`).join("")}</select></div>`;
    if (c.choice?.kind === "apt") body += note("APT 版本是发行版候选，不是目录中的占位版本 apt。状态 API 不提供 apt-get 可用性及候选包版本；平台检查依据系统服务能力与发行版元数据，最终包可用性由终端校验。");
    if (c.choice?.kind === "bom") body += note("仅使用现有已发布模板及签名归档，不创建或发布模板。后端任务执行时读取最新模板，不能持久锁定此版本；执行完成前请勿修改该模板。播放中的终端本轮阻塞。", "warn");
    for (const r of c.rows || []) {
      body += `<section class="mt12"><h3>${esc(r.alias)} ${badge(r.reasons.length ? "已阻塞" : "预览通过", r.reasons.length ? "warn" : "info")}</h3><div class="kv">${kvRow("终端 / OS / 架构", esc(`${r.id} / ${r.os} / ${r.arch}`))}${kvRow("当前已装版本 / 来源", r.current.map(esc).join("<br>"))}${kvRow("目标版本 / 包", r.targets.map(esc).join("<br>") || "—")}${kvRow("会改变什么", r.changes.map(esc).join("<br>") || "无待执行变更")}</div>${r.reasons.map(s => note(s, "warn")).join("")}</section>`;
    }
    if (c.choice && !valid(c) && !c.finished) body += note("本轮存在阻塞或无需安装的目标，整体禁止执行，不会静默跳过。请回列表重新选择有效目标，或更换安装来源。", "warn");
    if (c.results) body += `<table class="tbl mt12"><thead><tr><th>终端</th><th>提交结果（不是安装完成）</th></tr></thead><tbody>${c.results.map(r => `<tr><td>${esc(r.alias)}<div class="sub">${esc(r.id)}</div></td><td>${esc(r.detail)}${r.tasks.length ? `<div class="mono">${r.tasks.map(esc).join("<br>")}</div>` : ""}</td></tr>`).join("")}</tbody></table>` + note("不会自动重试任何已提交或结果不明请求。请在任务页核对服务端记录与终端执行结果后，再决定下一步。");
    if (!settingsAdmin()) body += note("只读预览可用；安装必须先到设置页解锁管理员，再返回重新预览。", "warn");
    setModalBody(body);
    setModalFoot(`<button class="btn ghost" data-act="ti-close">${c.busy ? "关闭（停止后续提交）" : "关闭"}</button><div class="spacer"></div>${!settingsAdmin() ? '<button class="btn ghost" data-act="ti-unlock">到设置解锁提示</button>' : ""}${!c.finished ? `<button class="btn ghost" data-act="ti-refresh" ${disabled ? "disabled" : ""}>刷新预览</button><button class="btn primary" data-act="ti-submit" ${disabled || writing || !settingsAdmin() || !valid(c) ? "disabled" : ""}>${c.busy ? "正在复核 / 提交…" : !settingsAdmin() ? "请先到设置解锁" : `确认安装 ${c.ids.length} 台…`}</button>` : '<button class="btn primary" data-act="ti-tasks">查看任务</button>'}`);
  }
  async function reload(c) {
    if (!here(c) || c.busy || c.loading || c.finished) return;
    c.loading = true; c.error = ""; render(c);
    try {
      const [base] = await Promise.all([readBase(c.ids), settingsRefreshSession()]);
      if (!here(c)) return;
      const choice = base.options.find(o => o.key === c.key), meta = await metadata(choice);
      if (!here(c)) return;
      apply(c, base, c.key, meta);
    } catch (e) { if (here(c)) c.error = message(e); }
    finally { c.loading = false; render(c); }
  }
  async function choose(el) {
    const c = active;
    if (!c || !here(c) || c.busy || c.loading || c.finished) return;
    c.loading = true; c.key = el.value; c.error = ""; c.choice = null; c.rows = []; render(c);
    try {
      const choice = c.base.options.find(o => o.key === c.key), meta = await metadata(choice);
      if (here(c)) apply(c, c.base, c.key, meta);
    } catch (e) { if (here(c)) c.error = message(e); }
    finally { c.loading = false; render(c); }
  }
  async function recheck(c, expected) {
    await settingsRefreshSession();
    if (!here(c)) return false;
    if (!settingsAdmin()) { c.error = "管理员会话未确认或已失效，请到设置解锁；未提交安装。"; return false; }
    const base = await readBase(c.ids);
    if (!here(c)) return false;
    const choice = base.options.find(o => o.key === c.key), meta = await metadata(choice);
    if (!here(c)) return false;
    apply(c, base, c.key, meta);
    if (expected !== c.fingerprint || !valid(c)) {
      c.error = "目标状态、版本或安装来源已变化，已取消本次提交。请核对刷新后的预览并重新确认。";
      return false;
    }
    return true;
  }
  async function submit() {
    const c = active;
    if (!c || !here(c) || c.busy || c.loading || c.finished || writing || !valid(c)) return;
    c.busy = true; c.error = ""; render(c);
    try {
      const expected = c.fingerprint;
      if (!await recheck(c, expected)) return;
      render(c);
      const confirmed = await confirmDlg({ title: `确认向 ${c.ids.length} 台终端安装`,
        message: `${c.choice.label}\n${c.rows.map(r => `${r.alias} (${r.id})：${r.changes.join("；")}`).join("\n")}\n仅提交以上 ${c.ids.length} 台，不跳过目标、不自动重试。${c.choice.kind === "bom" ? "任务按执行时最新模板运行，完成前请勿修改模板。" : "候选精确版本由各终端 APT 解析。"}`,
        okText: `向 ${c.ids.length} 台提交`, danger: true });
      if (!confirmed || !here(c) || !await recheck(c, expected)) return;
      c.finished = true; writing = true;
      c.results = c.rows.map(r => ({ id: r.id, alias: r.alias, tasks: [], detail: "尚未提交" }));
      render(c);
      for (const result of c.results) {
        if (!here(c)) break;
        try {
          const saveTask = response => {
            if (!response || typeof response.id !== "string" || !response.id) throw new Error("未返回任务 ID，提交结果不明");
            result.tasks.push(response.id);
          };
          if (c.choice.kind === "apt") {
            result.detail = "正在提交源配置"; render(c);
            try { saveTask(await api("POST", `/nodes/${enc(result.id)}/apt-sources/apply`)); }
            catch (e) { if (!(e.status === 400 && e.message === noSources)) throw e; }
            if (!here(c)) { result.detail = "窗口已关闭，未提交后续安装；请核对已提交的源配置"; break; }
            result.detail = "正在提交安装命令"; render(c);
            saveTask(await api("POST", `/nodes/${enc(result.id)}/commands`, { type: "apt-install", params: { packages: packages(c.choice.data.version) } }));
          } else {
            const b = c.choice.data;
            result.detail = "正在提交部署任务"; render(c);
            saveTask(await api("POST", "/jobs", { node_id: result.id, bom_id: b.bom_id, bom_version: b.version, kind: "provision" }));
          }
          result.detail = "提交成功，等待终端执行（未宣称安装完成）";
        } catch (e) {
          result.detail = `${e.status >= 400 && e.status < 500 ? "提交失败" : "提交结果不明"}：${message(e)}${result.tasks.length ? "；已有前置任务已提交，请勿重复执行" : ""}；不会自动重试`;
        }
        render(c);
      }
      if (here(c) && typeof refreshTerminalCenter === "function") {
        try { await refreshTerminalCenter(); } catch (e) { if (here(c)) c.error = "列表刷新失败，请到任务页核对；安装请求不会重试。"; }
      }
    } catch (e) { if (here(c)) c.error = message(e); }
    finally { if (c.finished) writing = false; c.busy = false; render(c); }
  }

  window.openTerminalInstall = async function (ids) {
    if (!Array.isArray(ids) || !ids.length || ids.some(id => typeof id !== "string" || !id.trim())) {
      toast("请从终端列表选择至少一台终端；不会默认全选。", "warn"); return;
    }
    closeModal();
    const c = { epoch: ++epoch, ids: Object.freeze([...new Set(ids)]), key: "", rows: [], busy: false, loading: false, finished: false, closed: false };
    active = c;
    openModal(`${icon("download", 18)} 安装终端服务`, "", "", { onClose: () => {
      c.closed = true;
      if (active === c) { active = null; epoch++; }
    } });
    await reload(c);
  };
  Object.assign(ACT, {
    "ti-submit": submit,
    "ti-refresh": () => active && reload(active),
    "ti-close": () => { if (active && here(active)) closeModal(); },
    "ti-unlock": () => toast("请关闭安装窗口，到设置页解锁管理员，再返回终端列表重新预览。", "warn"),
    "ti-tasks": () => {
      if (!active || !here(active) || !active.finished || active.busy) return;
      closeModal();
      if (typeof showTerminalTasks === "function") return showTerminalTasks();
      toast("请从终端中心查看任务记录。", "info");
    }
  });
  CHG["ti-source"] = choose;
})();
