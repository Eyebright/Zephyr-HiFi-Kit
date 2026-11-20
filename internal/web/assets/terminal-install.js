"use strict";

(() => {
  let epoch = 0, active = null, writing = false;
  const tiT = (key, params) => I18n.t("features." + key, params);
  const localized = (key, params) => ({ key, params });
  const display = value => typeof value === "string" ? value : tiT(value.key, value.params);
  const list = v => Array.isArray(v) ? v : [];
  const text = v => typeof v === "string" ? v : "";
  const message = e => e?.message || tiT("readFailed");
  const stable = v => JSON.stringify(v, function (key, value) {
    return value && typeof value === "object" && !Array.isArray(value)
      ? Object.keys(value).sort().reduce((out, k) => { out[k] = value[k]; return out; }, {}) : value;
  });
  const here = c => active === c && c.epoch === epoch && !c.closed;
  const note = (s, kind = "info") => `<div class="banner ${kind}" role="status">${icon(kind === "err" || kind === "warn" ? "alert" : "info", 15)}<span>${esc(display(s))}</span></div>`;
  const packages = v => [...new Set([v.apt_package, ...list(v.deps_pkgs)].filter(Boolean))];
  const packageName = s => text(s).replace(/\.service$/, "");
  const aptOS = s => /\b(debian|raspbian|ubuntu|linuxmint|linux mint|dietpi|moode|buster|bullseye|bookworm|trixie|focal|jammy|noble)\b/i.test(s);
  const nonLinux = s => /(?:^|[^a-z])(windows|android|darwin|macos|mac os|freebsd|openbsd)(?:$|[^a-z])/i.test(s);
  const supportsDistro = (v, os) => list(v.distro_support).some(d => {
    const term = text(d).trim().toLowerCase();
    return term && (" " + os.toLowerCase().replace(/[^a-z0-9.+-]+/g, " ") + " ").includes(" " + term + " ");
  });
  const sourceLabel = source => source === "system" ? tiT("sourceSystem") : ["server", "managed"].includes(source) ? tiT("sourceServer") : source || tiT("sourceNotReported");
  const appStatus = (id, status) => tiT("tiAppStatus", { id, status });
  const aptLabel = a => tiT("tiAPTLabel", { name: a.app.name || a.app.app_id, package: a.version.apt_package || tiT("tiUndeclared"), dependencies: list(a.version.deps_pkgs).join(" / ") || tiT("tiNoDependencies"), upstream: a.app.upstream ? tiT("tiUpstream", { upstream: a.app.upstream }) : "" });
  const bomLabel = b => `${b.bom_id}@${b.version} · ${list(b.apps).map(a => `${a.app_id}@${a.version || a.channel || tiT("tiUnpinnedVersion")}`).join(" / ") || tiT("tiNoApps")}`;

  async function read(path, array = false) {
    try {
      const value = await api("GET", path);
      if (array && !Array.isArray(value)) throw new Error(tiT("tiNotArray"));
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
      options.push({ key: stable(["apt", a.app.app_id, a.version.id || a.version.version, a.version.arch]), kind: "apt", data: a, get label() { return aptLabel(a); } });
    }
    for (const b of list(boms.value)) {
      if (b?.status !== "published" || !b.bom_id || !Number.isInteger(b.version) || b.version < 1) continue;
      options.push({ key: stable(["bom", b.bom_id, b.version]), kind: "bom", data: b, get label() { return bomLabel(b); } });
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
      label: appStatus(id, !known ? tiT("tiUnknownInstall")
        : records.length ? `${versions.join(" / ") || tiT("tiInstalledNoVersion")} · ${sources.map(sourceLabel).join(" / ") || tiT("sourceNotReported")}`
          : tiT(services.some(s => s.installed === false) ? "tiNotInstalled" : "tiNotDetected"))
    };
  }
  function artifactProblems(v, node, os, kind) {
    const reasons = [];
    if (!v) return [tiT("tiMissingArtifact")];
    if (v.arch !== node.arch && v.arch !== "all") reasons.push(tiT("tiArchMismatch", { artifact: v.arch || tiT("unknown"), node: node.arch || tiT("unknown") }));
    if (v.banned || v.lifecycle === "revoked") reasons.push(tiT("tiVersionRevoked"));
    if (list(v.distro_support).length && !supportsDistro(v, os)) reasons.push(tiT("tiDistroUnsupported", { distros: v.distro_support.join(" / ") }));
    if (v.glibc_min) reasons.push(tiT("tiGlibcUnverified", { version: v.glibc_min }));
    if (kind === "apt") {
      if (v.format !== "apt" || !v.apt_package || packages(v).some(p => !/^[a-z0-9][a-z0-9+.-]*$/.test(p))) reasons.push(tiT("tiInvalidAPT"));
    } else {
      if (v.arch === "all" && node.arch !== "all") reasons.push(tiT("tiAllArchUnsupported"));
      if (v.format === "apt") reasons.push(tiT("tiBOMContainsAPT"));
      else if (!["deb", "tar.bz2"].includes(v.format)) reasons.push(tiT("tiFormatUnsupported", { format: v.format || tiT("unknown") }));
      if (v.signed !== true || !v.signature || !v.artifact_path || !v.sha256) reasons.push(tiT("tiSignatureIncomplete"));
      if (nonLinux(v.artifact_path || "")) reasons.push(tiT("tiNonLinuxArtifact"));
      if ((v.format === "deb" && !/\.deb$/i.test(v.artifact_path || "")) || (v.format === "tar.bz2" && !/\.tar\.bz2$/i.test(v.artifact_path || ""))) reasons.push(tiT("tiFormatMismatch"));
      if (v.format === "tar.bz2" && !list(v.distro_support).length) reasons.push(tiT("tiDistroUndeclared"));
    }
    return reasons;
  }
  function preview(base, choice, meta) {
    return base.targets.map(target => {
      const node = list(base.nodes.value).find(n => n.node_id === target.id) || {};
      const status = target.status.value, identity = status?.identity;
      const os = [node.distro, status?.metrics?.distro].filter(Boolean).join(" / ") || tiT("tiOSUnknown");
      const platformOS = text(status?.metrics?.distro) || text(node.distro);
      const row = { id: target.id, alias: node.alias || target.id, os, arch: node.arch || tiT("unknown"), current: [], changes: [], targets: [], reasons: [], same: [] };
      if (base.nodes.error) row.reasons.push(tiT("tiNodesFailed", { message: base.nodes.error }));
      else if (!node.node_id) row.reasons.push(tiT("tiNodeMissing"));
      if (node.status !== "online") row.reasons.push(tiT("tiNodeOffline"));
      if (node.authorized !== true) row.reasons.push(tiT("tiUnauthorized"));
      if (target.apps.error) row.reasons.push(tiT("tiVersionsFailed", { message: target.apps.error }));
      if (target.status.error) row.reasons.push(tiT("tiLocalStatusFailed", { message: target.status.error }));
      else if (!identity || !Array.isArray(status.services)) row.reasons.push(tiT("tiLocalStatusIncomplete"));
      if (identity?.system_available !== true) row.reasons.push(tiT("tiSystemUnavailable"));
      if (nonLinux(os) || !aptOS(platformOS)) row.reasons.push(tiT("tiPlatformUnconfirmed"));
      if (!["amd64", "arm64", "armhf", "armv7hf"].includes(node.arch)) row.reasons.push(tiT("tiArchUnsupported"));
      if (identity?.node_id && identity.node_id !== target.id) row.reasons.push(tiT("tiIdentityMismatch"));
      if ((identity?.arch && identity.arch !== node.arch) || (status?.metrics?.arch && status.metrics.arch !== node.arch)) row.reasons.push(tiT("tiArchReportsMismatch"));
      if (typeof node.playing !== "boolean" || typeof identity?.playing !== "boolean") row.reasons.push(tiT("tiPlaybackUnconfirmed"));
      if (node.playing || identity?.playing) row.reasons.push(tiT("tiPlaybackProtected"));
      if (!choice) {
        row.current = target.status.error || target.apps.error || !Array.isArray(status?.services)
          ? [tiT("tiUnknownNotAbsent")]
          : [...list(target.apps.value).map(a => appStatus(a.app_id, `${a.version || tiT("versionNotReported")} · ${sourceLabel(a.source)}`)), ...status.services.filter(s => s.installed).map(s => appStatus(s.app_id, `${s.version || tiT("versionNotReported")} · ${sourceLabel(s.source)}`))];
        if (!row.current.length) row.current.push(tiT("tiNotDetected"));
        row.reasons.push(tiT("tiChooseSource"));
        return row;
      }
      let refs;
      if (choice.kind === "apt") refs = [{ app_id: choice.data.app.app_id, version: choice.data.version.version, artifact: choice.data.version }];
      else {
        const b = choice.data;
        if (meta.latest?.error) row.reasons.push(tiT("tiLatestTemplateFailed", { message: meta.latest.error }));
        else if (stable(meta.latest?.value) !== stable(b)) row.reasons.push(tiT("tiTemplateChanged"));
        refs = list(b.apps).map(a => {
          const versions = list(meta.versions?.[a.app_id]?.value).filter(v => v.app_id === a.app_id && v.version === a.version);
          return { ...a, artifact: versions.find(v => v.arch === node.arch) || versions.find(v => v.arch === "all") };
        });
        if (!refs.length) row.reasons.push(tiT("tiTemplateEmpty"));
      }
      for (const ref of refs) {
        const v = ref.artifact, state = installed(target, ref.app_id, v);
        row.current.push(state.label);
        if (!ref.version) row.reasons.push(appStatus(ref.app_id, tiT("tiTemplateUnpinned")));
        if (meta.versions?.[ref.app_id]?.error) row.reasons.push(appStatus(ref.app_id, tiT("tiMetadataFailed", { message: meta.versions[ref.app_id].error })));
        row.reasons.push(...artifactProblems(v, node, platformOS, choice.kind).map(s => appStatus(ref.app_id, s)));
        const affected = new Set([ref.app_id, ...list(v?.deps_apps), ...list(v?.deps_pkgs), ...list(v?.units).map(packageName)]);
        for (const service of list(status?.services)) if (service.app_id === ref.app_id) list(service.helpers).forEach(id => affected.add(id));
        for (const id of affected) {
          const dep = id === ref.app_id ? state : installed(target, id);
          if (id !== ref.app_id && dep.present) row.current.push(dep.label);
          if (dep.system) row.reasons.push(appStatus(id, tiT("tiSystemOwned")));
          if (dep.unknownSource) row.reasons.push(appStatus(id, tiT("tiUnknownOwner")));
          if (dep.conflict) row.reasons.push(appStatus(id, tiT("tiInventoryConflict")));
        }
        if (choice.kind === "apt") {
          row.targets.push(tiT("tiAPTCandidate", { packages: packages(v).join(" / ") }));
          if (state.present) row.reasons.push(appStatus(ref.app_id, tiT("tiAPTAlreadyInstalled")));
          else row.changes.push(tiT("tiAPTChanges", { packages: packages(v).join(" / ") }));
        } else {
          row.targets.push(`${ref.app_id}@${ref.version || tiT("tiUnpinned")} · ${v?.format || tiT("tiFormatUnknown")}${v?.deps_pkgs?.length ? tiT("tiDependencies", { packages: v.deps_pkgs.join(" / ") }) : ""}`);
          if (state.present && state.versions.length === 1 && state.versions[0] === ref.version) row.same.push(ref.app_id);
          else row.changes.push(tiT("tiBOMChanges", { action: tiT(state.present ? "tiUpdate" : "tiInstall"), app: `${ref.app_id}@${ref.version}`, dependencies: v?.deps_pkgs?.length ? tiT("tiDependencyChanges", { packages: v.deps_pkgs.join(" / ") }) : "" }));
        }
      }
      if (row.same.length) row.reasons.push(row.same.length === refs.length
        ? tiT("tiAllInstalled")
        : tiT("tiUnnecessaryReinstall", { apps: row.same.join(" / ") }));
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
    // Rebuild display-only descriptions using the current locale, without fetching or changing targets.
    const rows = c.base && !c.loading ? preview(c.base, c.choice, c.meta || {}) : c.rows || [];
    let body = note(tiT("tiFixedTargets", { count: I18n.number(c.ids.length) }));
    if (c.loading && !c.base) body += note(tiT("tiLoading"));
    if (c.error) body += note(c.error, "err");
    for (const [nameKey, result] of [["tiAPTServices", c.base?.apt], ["tiPublishedTemplates", c.base?.boms]]) if (result?.error) body += note(tiT("tiSourceReadFailed", { name: tiT(nameKey), message: result.error }), "warn");
    body += `<div class="field"><label for="ti-source">${esc(tiT("tiUnifiedSource"))}</label><select class="inp" id="ti-source" data-change="ti-source" ${disabled ? "disabled" : ""}><option value="">${esc(tiT("tiSourcePlaceholder"))}</option>${["apt", "bom"].map(kind => `<optgroup label="${esc(tiT(kind === "apt" ? "tiAPTServices" : "tiBOMSource"))}">${options.filter(o => o.kind === kind).map(o => `<option value="${esc(o.key)}" ${o.key === c.key ? "selected" : ""}>${esc(o.label)}</option>`).join("")}</optgroup>`).join("")}</select></div>`;
    if (c.choice?.kind === "apt") body += note(tiT("tiAPTNote"));
    if (c.choice?.kind === "bom") body += note(tiT("tiBOMNote"), "warn");
    for (const r of rows) {
      body += `<section class="mt12"><h3>${esc(r.alias)} ${badge(tiT(r.reasons.length ? "tiBlocked" : "tiPreviewPassed"), r.reasons.length ? "warn" : "info")}</h3><div class="kv">${kvRow(tiT("tiNodePlatform"), esc(`${r.id} / ${r.os} / ${r.arch}`))}${kvRow(tiT("tiCurrentVersions"), r.current.map(esc).join("<br>"))}${kvRow(tiT("tiTargetVersions"), r.targets.map(esc).join("<br>") || "—")}${kvRow(tiT("tiChanges"), r.changes.map(esc).join("<br>") || esc(tiT("tiNoChanges")))}</div>${r.reasons.map(s => note(s, "warn")).join("")}</section>`;
    }
    if (c.choice && !valid(c) && !c.finished) body += note(tiT("tiBlockedNote"), "warn");
    if (c.results) body += `<table class="tbl mt12"><thead><tr><th>${esc(tiT("node"))}</th><th>${esc(tiT("tiSubmissionResult"))}</th></tr></thead><tbody>${c.results.map(r => `<tr><td>${esc(r.alias)}<div class="sub">${esc(r.id)}</div></td><td>${esc(display(r.detail))}${r.tasks.length ? `<div class="mono">${r.tasks.map(esc).join("<br>")}</div>` : ""}</td></tr>`).join("")}</tbody></table>` + note(tiT("tiNoRetryNote"));
    if (!settingsAdmin()) body += note(tiT("tiReadOnly"), "warn");
    setModalBody(body);
    setModalFoot(`<button class="btn ghost" data-act="ti-close">${esc(tiT(c.busy ? "tiCloseStop" : "close"))}</button><div class="spacer"></div>${!settingsAdmin() ? `<button class="btn ghost" data-act="ti-unlock">${esc(tiT("tiUnlockHint"))}</button>` : ""}${!c.finished ? `<button class="btn ghost" data-act="ti-refresh" ${disabled ? "disabled" : ""}>${esc(tiT("tiRefreshPreview"))}</button><button class="btn primary" data-act="ti-submit" ${disabled || writing || !settingsAdmin() || !valid(c) ? "disabled" : ""}>${esc(c.busy ? tiT("tiSubmitting") : !settingsAdmin() ? tiT("tiUnlockFirst") : tiT("tiConfirmInstall", { count: I18n.number(c.ids.length) }))}</button>` : `<button class="btn primary" data-act="ti-tasks">${esc(tiT("tiViewTasks"))}</button>`}`);
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
    if (!settingsAdmin()) { c.error = localized("tiSessionInvalid"); return false; }
    const base = await readBase(c.ids);
    if (!here(c)) return false;
    const choice = base.options.find(o => o.key === c.key), meta = await metadata(choice);
    if (!here(c)) return false;
    apply(c, base, c.key, meta);
    if (expected !== c.fingerprint || !valid(c)) {
      c.error = localized("tiRecheckChanged");
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
      const confirmed = await confirmDlg({ title: tiT("tiConfirmTitle", { count: I18n.number(c.ids.length) }),
        message: tiT("tiConfirmMessage", { source: c.choice.label, changes: c.rows.map(r => tiT("tiConfirmNode", { name: r.alias, id: r.id, changes: r.changes.join("; ") })).join("\n"), count: I18n.number(c.ids.length), note: tiT(c.choice.kind === "bom" ? "tiConfirmBOM" : "tiConfirmAPT") }),
        okText: tiT("tiSubmitCount", { count: I18n.number(c.ids.length) }), danger: true });
      if (!confirmed || !here(c) || !await recheck(c, expected)) return;
      c.finished = true; writing = true;
      c.results = c.rows.map(r => ({ id: r.id, alias: r.alias, tasks: [], detail: localized("tiNotSubmitted") }));
      render(c);
      for (const result of c.results) {
        if (!here(c)) break;
        try {
          const saveTask = response => {
            if (!response || typeof response.id !== "string" || !response.id) throw new Error(tiT("tiMissingTaskID"));
            result.tasks.push(response.id);
          };
          if (c.choice.kind === "apt") {
            result.detail = localized("tiSubmittingSources"); render(c);
            try { saveTask(await api("POST", `/nodes/${enc(result.id)}/apt-sources/apply`)); }
            catch (e) { if (!(e.status === 400 && e.code === "apt_sources_empty")) throw e; }
            if (!here(c)) { result.detail = localized("tiClosedBeforeInstall"); break; }
            result.detail = localized("tiSubmittingCommand"); render(c);
            saveTask(await api("POST", `/nodes/${enc(result.id)}/commands`, { type: "apt-install", params: { packages: packages(c.choice.data.version) } }));
          } else {
            const b = c.choice.data;
            result.detail = localized("tiSubmittingJob"); render(c);
            saveTask(await api("POST", "/jobs", { node_id: result.id, bom_id: b.bom_id, bom_version: b.version, kind: "provision" }));
          }
          result.detail = localized("tiSubmitted");
        } catch (e) {
          result.detail = localized("tiSubmitError", { get status() { return tiT(e.status >= 400 && e.status < 500 ? "tiSubmitFailed" : "tiSubmitUnknown"); }, message: message(e), get prerequisite() { return result.tasks.length ? tiT("tiPrerequisiteSubmitted") : ""; } });
        }
        render(c);
      }
      if (here(c) && typeof refreshTerminalCenter === "function") {
        try { await refreshTerminalCenter(); } catch (e) { if (here(c)) c.error = localized("tiRefreshFailed"); }
      }
    } catch (e) { if (here(c)) c.error = message(e); }
    finally { if (c.finished) writing = false; c.busy = false; render(c); }
  }

  window.openTerminalInstall = async function (ids) {
    if (!Array.isArray(ids) || !ids.length || ids.some(id => typeof id !== "string" || !id.trim())) {
      toast(tiT("tiSelectTargets"), "warn"); return;
    }
    closeModal();
    const c = { epoch: ++epoch, ids: Object.freeze([...new Set(ids)]), key: "", rows: [], busy: false, loading: false, finished: false, closed: false };
    active = c;
    openModal(`${icon("download", 18)} ${esc(tiT("tiTitle"))}`, "", "", { onClose: () => {
      c.closed = true;
      if (active === c) { active = null; epoch++; }
    } });
    await reload(c);
  };
  Object.assign(ACT, {
    "ti-submit": submit,
    "ti-refresh": () => active && reload(active),
    "ti-close": () => { if (active && here(active)) closeModal(); },
    "ti-unlock": () => toast(tiT("tiUnlockInstructions"), "warn"),
    "ti-tasks": () => {
      if (!active || !here(active) || !active.finished || active.busy) return;
      closeModal();
      if (typeof showTerminalTasks === "function") return showTerminalTasks();
      toast(tiT("tiTasksHint"), "info");
    }
  });
  CHG["ti-source"] = choose;
})();
