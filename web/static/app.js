const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];
const state = {
  token: localStorage.getItem("xpanel_token") || "",
  meta: null,
  servers: [],
  plans: [],
  certs: [],
  authMode: "login",
  srvFilter: "all",
  me: null,
};

async function api(path, opts = {}) {
  const headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
  if (state.token) headers.Authorization = "Bearer " + state.token;
  const res = await fetch(path, { ...opts, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function fmtBytes(n) {
  n = Number(n) || 0;
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(i ? 2 : 0) + " " + u[i];
}
function fmtSpeed(n) {
  return fmtBytes(n) + "/s";
}
function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}
// —— 妙妙屋同款主题：light → dark → system 循环 ——
const THEME_MODES = ["light", "dark", "system"];
const THEME_LABELS = {
  light: "浅色模式",
  dark: "深色模式",
  system: "跟随系统",
};
const THEME_ICONS = {
  light: "☀",
  dark: "☾",
  system: "◐",
};

function resolveTheme(mode) {
  if (mode === "light") return "light";
  if (mode === "dark") return "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function showThemeToast(text) {
  const el = $("#theme-toast");
  if (!el) return;
  el.textContent = text;
  el.classList.add("show");
  clearTimeout(showThemeToast._t);
  showThemeToast._t = setTimeout(() => el.classList.remove("show"), 2600);
}

function setTheme(mode, opts = {}) {
  const quiet = !!opts.quiet;
  let modeKey = mode || "system";
  // 兼容旧值
  if (modeKey === "auto" || modeKey === "anime" || modeKey === "flat" || String(modeKey).startsWith("auto-")) {
    modeKey = "system";
  }
  if (!THEME_MODES.includes(modeKey)) modeKey = "system";
  localStorage.setItem("xpanel_theme_mode", modeKey);

  const applied = resolveTheme(modeKey);
  document.documentElement.classList.toggle("dark", applied === "dark");
  document.documentElement.removeAttribute("data-theme");

  const meta = document.querySelector("meta[name='theme-color']");
  if (meta) meta.setAttribute("content", applied === "dark" ? "#10131c" : "#fffaf7");

  const label = THEME_LABELS[modeKey] || modeKey;
  const ico = $("#theme-ico");
  const lab = $("#theme-label");
  if (ico) ico.textContent = THEME_ICONS[modeKey] || "◐";
  if (lab) lab.textContent = label;
  const cycle = $("#theme-cycle");
  if (cycle) {
    cycle.title = label;
    cycle.setAttribute("aria-label", label);
  }
  const hint = $("#theme-hint");
  if (hint) {
    hint.textContent = modeKey === "system"
      ? `跟随系统 · 当前${applied === "dark" ? "深色" : "浅色"}`
      : `主题 · ${label}`;
  }
  if (!quiet) showThemeToast(label);

  const sel = $("#set-theme");
  if (sel) sel.value = modeKey;
  return modeKey;
}

function cycleTheme() {
  const cur = localStorage.getItem("xpanel_theme_mode") || "system";
  // 与妙妙屋 ThemeSwitch 一致：light → dark → system → light
  let next = "light";
  if (cur === "light") next = "dark";
  else if (cur === "dark") next = "system";
  else next = "light";
  setTheme(next);
}

function watchSystemTheme() {
  if (watchSystemTheme._mq) return;
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => {
    const mode = localStorage.getItem("xpanel_theme_mode") || "system";
    if (mode === "system") setTheme("system", { quiet: true });
  };
  if (mq.addEventListener) mq.addEventListener("change", onChange);
  else if (mq.addListener) mq.addListener(onChange);
  watchSystemTheme._mq = mq;
}

async function boot() {
  // 兼容旧 key
  let mode = localStorage.getItem("xpanel_theme_mode");
  if (!mode) {
    const legacy = localStorage.getItem("xpanel_theme");
    if (legacy === "light" || legacy === "dark") mode = legacy;
    else mode = "system";
    localStorage.setItem("xpanel_theme_mode", mode);
  }
  setTheme(mode, { quiet: true });
  watchSystemTheme();

  const meta = await api("/api/meta");
  state.meta = meta;
  if ($("#ver")) $("#ver").textContent = "v" + meta.version;
  if (!meta.initialized) {
    $("#auth-hint").textContent = "首次启动：创建管理员账号";
    $("#auth-tabs").classList.add("hidden");
    $("#auth-btn").textContent = "创建并登录";
    showAuth();
    return;
  }
  if (!state.token) {
    $("#auth-hint").textContent = "登录或使用邀请码注册";
    showAuth();
    return;
  }
  try { await enterMain(); }
  catch { state.token = ""; localStorage.removeItem("xpanel_token"); showAuth(); }
}

function showAuth() {
  $("#view-auth").classList.remove("hidden");
  $("#view-main").classList.add("hidden");
}

async function enterMain() {
  $("#view-auth").classList.add("hidden");
  $("#view-main").classList.remove("hidden");
  const me = await api("/api/auth/me");
  state.me = me;
  $("#nav-user").textContent = me.user.username + " · " + me.user.role;
  const o = location.origin;
  const tok = me.user.subscribe_token;
  const fill = (id, path) => {
    const el = $(id);
    if (!el) return;
    el.textContent = (path && path.startsWith("http") ? path : o + path);
  };
  fill("#sub-url", me.subscribe_url || ("/sub/" + tok));
  fill("#sub-clash", me.subscribe_clash || ("/sub/" + tok + "/clash"));
  fill("#sub-singbox", me.subscribe_singbox || ("/sub/" + tok + "/singbox"));
  fill("#sub-surge", me.subscribe_surge || ("/sub/" + tok + "/surge"));
  if (me.subscribe_short || me.short_code) {
    // append short link into surge card area if present
    const surge = $("#sub-surge");
    if (surge) {
      const short = me.subscribe_short?.startsWith("http")
        ? me.subscribe_short
        : o + "/s/" + (me.short_code || "");
      surge.textContent = (me.subscribe_surge?.startsWith("http") ? me.subscribe_surge : o + "/sub/" + tok + "/surge") +
        "\n短码: " + short;
    }
  }
  await refreshDash();
  switchTab("dash");
}

// auth tabs
$$("#auth-tabs button").forEach((b) => {
  b.onclick = () => {
    state.authMode = b.dataset.mode;
    $$("#auth-tabs button").forEach((x) => x.classList.toggle("active", x === b));
    $("#invite-wrap").classList.toggle("hidden", state.authMode !== "register");
    $("#auth-btn").textContent = state.authMode === "register" ? "注册" : "登录";
  };
});

$("#auth-btn").onclick = async () => {
  $("#auth-err").textContent = "";
  try {
    if (!state.meta?.initialized) {
      const data = await api("/api/auth/setup", {
        method: "POST",
        body: JSON.stringify({ username: $("#username").value.trim(), password: $("#password").value }),
      });
      state.token = data.token;
    } else if (state.authMode === "register") {
      await api("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({
          username: $("#username").value.trim(),
          password: $("#password").value,
          code: $("#invite-code").value.trim(),
        }),
      });
      const data = await api("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ username: $("#username").value.trim(), password: $("#password").value }),
      });
      state.token = data.token;
    } else {
      const data = await api("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ username: $("#username").value.trim(), password: $("#password").value }),
      });
      state.token = data.token;
    }
    localStorage.setItem("xpanel_token", state.token);
    await enterMain();
  } catch (e) {
    $("#auth-err").textContent = e.message;
  }
};

$$("#nav .nav").forEach((btn) => { btn.onclick = () => switchTab(btn.dataset.tab); });
const themeCycleBtn = $("#theme-cycle");
if (themeCycleBtn) themeCycleBtn.onclick = () => cycleTheme();
$("#btn-logout").onclick = () => { localStorage.removeItem("xpanel_token"); location.reload(); };
$("#modal-close").onclick = () => $("#modal").classList.add("hidden");

function switchTab(name) {
  $$("#nav .nav").forEach((b) => b.classList.toggle("active", b.dataset.tab === name));
  $$(".tab").forEach((t) => t.classList.add("hidden"));
  const el = $("#tab-" + name);
  if (el) el.classList.remove("hidden");
  const loaders = {
    dash: refreshDash,
    servers: refreshServers,
    inbounds: async () => { await fillServerSelects(); await fillCertSelect(); await refreshInbounds(); },
    outbounds: async () => { await fillServerSelects(); await refreshOutbounds(); await refreshRoutes(); },
    tunnels: async () => { await fillServerSelects(); await refreshTunnels(); },
    plans: refreshPlans,
    users: async () => { await refreshPlans(); await refreshUsers(); },
    invites: async () => { await refreshPlans(); await refreshInvites(); },
    nodes: refreshExt,
    certs: async () => { await fillServerSelects(); await refreshCerts(); },
    nginx: async () => { await fillServerSelects(); },
    traffic: refreshTraffic,
    speed: () => Promise.resolve(),
    links: refreshLinks,
    settings: refreshSettings,
  };
  if (loaders[name]) loaders[name]().catch((e) => alert(e.message));
}

function drawChart(series) {
  const c = $("#traffic-chart");
  if (!c) return;
  const ctx = c.getContext("2d");
  const w = c.width, h = c.height;
  ctx.clearRect(0, 0, w, h);
  if (!series?.length) {
    ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue("--muted") || "#888";
    ctx.fillText("暂无数据", 20, h / 2);
    return;
  }
  const max = Math.max(1, ...series.map((x) => (x.up || 0) + (x.down || 0)));
  const pad = 20;
  const step = (w - pad * 2) / Math.max(1, series.length - 1);
  ctx.strokeStyle = "#7c6cff";
  ctx.lineWidth = 2;
  ctx.beginPath();
  series.forEach((p, i) => {
    const x = pad + i * step;
    const y = h - pad - ((p.up + p.down) / max) * (h - pad * 2);
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();
  ctx.fillStyle = "rgba(124,108,255,.15)";
  ctx.lineTo(pad + (series.length - 1) * step, h - pad);
  ctx.lineTo(pad, h - pad);
  ctx.closePath();
  ctx.fill();
}

async function refreshDash() {
  const d = await api("/api/dashboard");
  $("#dash-stats").innerHTML = [
    ["用户", d.users], ["服务器", d.servers], ["在线", d.online], ["离线", d.offline || 0],
    ["入站", d.inbounds], ["套餐", d.plans],
    ["上行", fmtBytes(d.traffic_up)], ["下行", fmtBytes(d.traffic_down)],
    ["实时↑", fmtSpeed(d.speed_up)], ["实时↓", fmtSpeed(d.speed_down)],
  ].map(([l, n]) => `<div class="stat"><div class="n">${n}</div><div class="l">${l}</div></div>`).join("");
  drawChart(d.series || []);
}
if ($("#btn-refresh-dash")) $("#btn-refresh-dash").onclick = () => refreshDash();

async function fillServerSelects() {
  const data = await api("/api/servers");
  state.servers = data.servers || [];
  for (const id of ["in-server", "ob-server", "rt-server", "acme-server", "tn-server", "ngx-server"]) {
    const sel = $("#" + id);
    if (!sel) continue;
    const head = id === "acme-server" ? '<option value="">全部 Agent</option>' : "";
    sel.innerHTML = head + state.servers.map((s) => `<option value="${s.id}">${escapeHtml(s.name)}</option>`).join("");
  }
}
async function fillCertSelect() {
  const data = await api("/api/certs");
  state.certs = data.certificates || [];
  const sel = $("#in-cert");
  if (!sel) return;
  sel.innerHTML = '<option value="0">无 TLS 证书</option>' +
    state.certs.filter((c) => c.status === "active").map((c) =>
      `<option value="${c.id}">${escapeHtml(c.domain)} (#${c.id})</option>`).join("");
}

$$("#srv-filter button").forEach((b) => {
  b.onclick = () => {
    state.srvFilter = b.dataset.f;
    $$("#srv-filter button").forEach((x) => x.classList.toggle("active", x === b));
    refreshServers();
  };
});

async function refreshServers() {
  const data = await api("/api/servers");
  state.servers = data.servers || [];
  let list = state.servers;
  if (state.srvFilter === "online") list = list.filter((s) => s.online);
  if (state.srvFilter === "offline") list = list.filter((s) => !s.online && s.status !== "pending");
  const box = $("#server-list");
  box.innerHTML = list.map((s) => {
    const chip = s.online ? "on" : s.status === "pending" ? "pending" : "off";
    const label = s.online ? "在线" : s.status === "pending" ? "待安装" : "离线";
    return `<div class="server-card">
      <div class="title">
        <strong><span class="dot ${chip}"></span>${escapeHtml(s.name)}</strong>
        <span class="chip ${chip}">${label} · ${escapeHtml(s.conn_mode || "http")}</span>
      </div>
      <div class="meta">${escapeHtml(s.hostname || "-")} · ${escapeHtml(s.domain || s.public_ip || "no-ip")}</div>
      <div class="meta badge-speed">↑${fmtBytes(s.traffic_up)} ↓${fmtBytes(s.traffic_down)} · 实时 ${fmtSpeed(s.speed_up)} / ${fmtSpeed(s.speed_down)}</div>
      <div class="meta">xray:${s.xray_running ? "on" : "off"} · cfg v${s.config_version} ${s.tags ? "· " + escapeHtml(s.tags) : ""}</div>
      ${s.agent_error ? `<div class="meta" style="color:var(--danger)">错误: ${escapeHtml(s.agent_error).slice(0,180)}</div>` : ""}
      <div class="row" style="margin:0">
        <button class="small" data-act="install" data-id="${s.id}">安装命令</button>
        <button class="small" data-act="bump" data-id="${s.id}">下发配置</button>
        <button class="small danger" data-act="del" data-id="${s.id}">删除</button>
      </div>
    </div>`;
  }).join("") || '<p class="muted">暂无服务器</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!confirm("删除服务器？")) return;
      await api("/api/servers/" + id, { method: "DELETE" });
      await refreshServers();
    }
    if (btn.dataset.act === "bump") {
      await api("/api/servers/" + id + "/bump-config", { method: "POST", body: "{}" });
      alert("已触发下发");
      await refreshServers();
    }
    if (btn.dataset.act === "install") {
      const info = await api("/api/servers/" + id + "/install-cmd");
      $("#modal-title").textContent = "安装 " + (info.name || "");
      $("#install-cmd").textContent =
        "# Linux 一键\n" + (info.one_click_cmd || info.install_cmd || "") +
        "\n\n# Docker\n" + info.docker_cmd +
        "\n\n# Binary\n" + info.binary_cmd;
      $("#modal").classList.remove("hidden");
    }
  };
}
$("#btn-add-server").onclick = async () => {
  const name = $("#server-name").value.trim();
  if (!name) return;
  const srv = await api("/api/servers", { method: "POST", body: JSON.stringify({ name }) });
  const domain = $("#server-domain").value.trim();
  if (domain && srv.server?.id) {
    await api("/api/servers/" + srv.server.id, { method: "PUT", body: JSON.stringify({ domain }) });
  }
  $("#server-name").value = "";
  $("#server-domain").value = "";
  await refreshServers();
};

async function refreshInbounds() {
  const data = await api("/api/inbounds");
  $("#inbound-list").innerHTML = (data.inbounds || []).map((inb) => `
    <div class="item"><div>
      <strong>${escapeHtml(inb.tag)}</strong> · ${escapeHtml(inb.protocol)} :${inb.port}
      ${inb.cert_id ? ' · <span class="chip">TLS#' + inb.cert_id + "</span>" : ""}
      <div class="meta">server ${escapeHtml(String(inb.server_id).slice(0, 8))}… · x${inb.multiplier || 1}</div>
    </div><button class="small danger" data-id="${inb.id}">删除</button></div>`).join("") || '<p class="muted">暂无入站</p>';
  $("#inbound-list").onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn || !confirm("删除？")) return;
    await api("/api/inbounds/" + btn.dataset.id, { method: "DELETE" });
    await refreshInbounds();
  };
}
$("#btn-add-in").onclick = async () => {
  const cert_id = Number($("#in-cert").value) || 0;
  try {
    const r = await api("/api/inbounds", {
      method: "POST",
      body: JSON.stringify({
        server_id: $("#in-server").value,
        protocol: $("#in-proto").value,
        port: Number($("#in-port").value),
        tag: $("#in-tag").value.trim() || undefined,
        cert_id, enable_tls: cert_id > 0,
      }),
    });
    let msg = "已创建入站 #" + r.id;
    if (r.share_link) msg += "\n\n分享链接:\n" + r.share_link;
    if (r.settings?.password) msg += "\n\nSS 密码: " + r.settings.password;
    if (r.settings?.clients?.[0]?.password) msg += "\n\nTrojan 密码: " + r.settings.clients[0].password;
    if (r.settings?.clients?.[0]?.id) msg += "\n\nUUID: " + r.settings.clients[0].id;
    alert(msg);
  } catch (e) {
    alert(e.message);
  }
  await refreshInbounds();
};
$("#btn-reality").onclick = async () => {
  const server_id = $("#in-server").value;
  if (!server_id) return alert("先选服务器");
  const r = await api("/api/inbounds/quick-reality", {
    method: "POST",
    body: JSON.stringify({ server_id, port: Number($("#in-port").value) || 443 }),
  });
  alert("Reality 已创建\nPublicKey:\n" + r.public_key + "\n\n分享链接:\n" + r.share_link);
  await refreshInbounds();
};

async function refreshOutbounds() {
  const data = await api("/api/outbounds");
  $("#ob-list").innerHTML = (data.outbounds || []).map((o) => `
    <div class="item"><div><strong>${escapeHtml(o.tag)}</strong> · ${escapeHtml(o.protocol)}
    <div class="meta">${o.enabled ? "已启用" : "未启用（不会下发，避免配置错误）"}</div></div>
    <div class="row" style="margin:0">
      <button class="small" data-act="toggle" data-id="${o.id}" data-en="${o.enabled ? 0 : 1}">${o.enabled ? "禁用" : "启用"}</button>
      <button class="small danger" data-act="del" data-id="${o.id}">删除</button>
    </div></div>`).join("") || '<p class="muted">暂无出站</p>';
  $("#ob-list").onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    if (btn.dataset.act === "del") {
      await api("/api/outbounds/" + btn.dataset.id, { method: "DELETE" });
    }
    if (btn.dataset.act === "toggle") {
      try {
        await api("/api/outbounds/" + btn.dataset.id, {
          method: "PUT",
          body: JSON.stringify({ enabled: btn.dataset.en === "1" }),
        });
      } catch (err) {
        alert("启用失败（可能密钥未填完整）: " + err.message);
      }
    }
    await refreshOutbounds();
  };
}
$("#btn-add-ob").onclick = async () => {
  await api("/api/outbounds", {
    method: "POST",
    body: JSON.stringify({
      server_id: $("#ob-server").value,
      tag: $("#ob-tag").value.trim(),
      protocol: $("#ob-proto").value,
      settings: {},
    }),
  });
  await refreshOutbounds();
};
$("#btn-warp").onclick = async () => {
  const server_id = $("#ob-server").value;
  if (!server_id) return;
  const r = await api("/api/outbounds/quick-warp", { method: "POST", body: JSON.stringify({ server_id }) });
  alert(r.note || "ok");
  await refreshOutbounds();
  await refreshRoutes();
};

async function refreshRoutes() {
  const data = await api("/api/routes");
  $("#rt-list").innerHTML = (data.routes || []).map((r) => `
    <div class="item"><div><strong>${escapeHtml(r.name)}</strong> → ${escapeHtml(r.outbound_tag)}
    <div class="meta">${escapeHtml(r.domain_json)}</div></div>
    <button class="small danger" data-id="${r.id}">删除</button></div>`).join("") || '<p class="muted">暂无路由</p>';
  $("#rt-list").onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/routes/" + btn.dataset.id, { method: "DELETE" });
    await refreshRoutes();
  };
}
$("#btn-add-rt").onclick = async () => {
  const domain = $("#rt-domain").value.split(/[,，\s]+/).filter(Boolean);
  await api("/api/routes", {
    method: "POST",
    body: JSON.stringify({
      server_id: $("#rt-server").value,
      outbound_tag: $("#rt-out").value.trim(),
      domain, name: $("#rt-out").value.trim(),
    }),
  });
  await refreshRoutes();
};

async function refreshTunnels() {
  const data = await api("/api/tunnels");
  $("#tn-list").innerHTML = (data.tunnels || []).map((t) => `
    <div class="item"><div><strong>${escapeHtml(t.name)}</strong> :${t.listen_port} → ${escapeHtml(t.target_host)}:${t.target_port}
    <div class="meta">${escapeHtml(t.protocol)} · ${escapeHtml(String(t.server_id).slice(0, 8))}</div></div>
    <button class="small danger" data-id="${t.id}">删除</button></div>`).join("") || '<p class="muted">暂无隧道</p>';
  $("#tn-list").onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/tunnels/" + btn.dataset.id, { method: "DELETE" });
    await refreshTunnels();
  };
}
$("#btn-add-tn").onclick = async () => {
  await api("/api/tunnels", {
    method: "POST",
    body: JSON.stringify({
      server_id: $("#tn-server").value,
      name: $("#tn-name").value.trim(),
      listen_port: Number($("#tn-listen").value),
      target_host: $("#tn-host").value.trim(),
      target_port: Number($("#tn-port").value),
    }),
  });
  await refreshTunnels();
};

async function refreshPlans() {
  const data = await api("/api/plans");
  state.plans = data.plans || [];
  for (const id of ["u-plan", "inv-plan"]) {
    const sel = $("#" + id);
    if (!sel) continue;
    const head = id === "u-plan" ? '<option value="0">无套餐</option>' : "";
    sel.innerHTML = head + state.plans.map((p) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join("");
  }
  const box = $("#plan-list");
  if (!box) return;
  box.innerHTML = state.plans.map((p) => `
    <div class="item"><div><strong>${escapeHtml(p.name)}</strong>
    <div class="meta">${fmtBytes(p.traffic_limit)} · ${p.duration_days}天 · ${escapeHtml(p.price_note || "")}</div></div>
    <button class="small danger" data-id="${p.id}">删除</button></div>`).join("") || '<p class="muted">暂无套餐</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/plans/" + btn.dataset.id, { method: "DELETE" });
    await refreshPlans();
  };
}
$("#btn-add-plan").onclick = async () => {
  await api("/api/plans", {
    method: "POST",
    body: JSON.stringify({
      name: $("#plan-name").value.trim(),
      traffic_limit: Number($("#plan-traffic").value) || 0,
      duration_days: Number($("#plan-days").value) || 30,
      price_note: $("#plan-note").value.trim(),
    }),
  });
  await refreshPlans();
};

async function refreshUsers() {
  const data = await api("/api/users");
  $("#user-list").innerHTML = (data.users || []).map((u) => {
    const lim = u.traffic_limit || 0;
    const pct = lim > 0 ? Math.min(100, Math.round((u.traffic_used / lim) * 100)) : 0;
    return `<div class="item"><div style="flex:1">
      <strong>${escapeHtml(u.username)}</strong> · ${escapeHtml(u.role)} ${u.enabled ? "" : "(禁用)"}
      <div class="meta">流量 ${fmtBytes(u.traffic_used)} / ${lim ? fmtBytes(lim) : "∞"}
      · 到期 ${u.expire_at ? new Date(u.expire_at * 1000).toLocaleDateString() : "-"} · plan ${u.plan_id || 0}</div>
      ${lim ? `<div class="progress"><i style="width:${pct}%"></i></div>` : ""}
    </div>
    <div class="row" style="margin:0">
      <button class="small" data-act="renew" data-id="${u.id}">+30天</button>
      <button class="small" data-act="reset" data-id="${u.id}">重置流量</button>
      <button class="small danger" data-act="del" data-id="${u.id}">删除</button>
    </div></div>`;
  }).join("") || '<p class="muted">暂无用户</p>';
  $("#user-list").onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!confirm("删除用户？")) return;
      await api("/api/users/" + id, { method: "DELETE" });
    }
    if (btn.dataset.act === "renew") await api("/api/users/" + id, { method: "PUT", body: JSON.stringify({ renew_days: 30 }) });
    if (btn.dataset.act === "reset") await api("/api/users/" + id, { method: "PUT", body: JSON.stringify({ reset_traffic: true }) });
    await refreshUsers();
  };
}
$("#btn-add-user").onclick = async () => {
  await api("/api/users", {
    method: "POST",
    body: JSON.stringify({
      username: $("#u-name").value.trim(),
      password: $("#u-pass").value,
      role: $("#u-role").value,
      plan_id: Number($("#u-plan").value) || 0,
    }),
  });
  $("#u-name").value = "";
  $("#u-pass").value = "";
  await refreshUsers();
};

async function refreshInvites() {
  const data = await api("/api/invites");
  $("#inv-list").innerHTML = (data.invites || []).map((i) => `
    <div class="item"><div><strong class="mono" style="padding:0;border:0;background:none">${escapeHtml(i.code)}</strong>
    <div class="meta">plan ${i.plan_id} · ${i.used_count}/${i.max_uses} · ${i.enabled ? "有效" : "关"}</div></div></div>`).join("") || '<p class="muted">暂无邀请码</p>';
}
$("#btn-add-inv").onclick = async () => {
  const r = await api("/api/invites", {
    method: "POST",
    body: JSON.stringify({
      plan_id: Number($("#inv-plan").value) || 0,
      max_uses: Number($("#inv-max").value) || 1,
      days: Number($("#inv-days").value) || 30,
      count: Number($("#inv-count").value) || 1,
    }),
  });
  alert("已生成: " + (r.codes || []).join(", "));
  await refreshInvites();
};

async function refreshExt() {
  const data = await api("/api/nodes/external");
  $("#ext-list").innerHTML = (data.nodes || []).map((n) => `
    <div class="item"><div><strong>${escapeHtml(n.name)}</strong> · ${escapeHtml(n.protocol)}
    <div class="meta">${escapeHtml(n.address)}:${n.port}</div></div>
    <button class="small danger" data-id="${n.id}">删除</button></div>`).join("") || '<p class="muted">暂无</p>';
  $("#ext-list").onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/nodes/external/" + btn.dataset.id, { method: "DELETE" });
    await refreshExt();
  };
}
$("#btn-import").onclick = async () => {
  const r = await api("/api/nodes/external", { method: "POST", body: JSON.stringify({ links: $("#ext-links").value }) });
  alert("导入 " + r.imported);
  $("#ext-links").value = "";
  await refreshExt();
};

async function refreshCerts() {
  const data = await api("/api/certs");
  $("#cert-list").innerHTML = (data.certificates || []).map((c) => {
    const exp = c.expire_at ? new Date(c.expire_at * 1000).toLocaleString() : "-";
    return `<div class="item"><div>
      <strong>${escapeHtml(c.name || c.domain)}</strong> · ${escapeHtml(c.status || "active")}
      <div class="meta">${escapeHtml(c.domain)} · ${escapeHtml(c.provider || "")} · 到期 ${exp}
      ${c.auto_renew ? " · 自动续期" : ""}${c.last_error ? " · " + escapeHtml(c.last_error) : ""}</div>
    </div>
    <div class="row" style="margin:0">
      <button class="small" data-act="deploy" data-id="${c.id}">下发</button>
      <button class="small" data-act="renew" data-id="${c.id}">续期</button>
      <button class="small danger" data-act="del" data-id="${c.id}">删除</button>
    </div></div>`;
  }).join("") || '<p class="muted">暂无证书</p>';
  $("#cert-list").onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    if (btn.dataset.act === "del") await api("/api/certs/" + btn.dataset.id, { method: "DELETE" });
    if (btn.dataset.act === "deploy") {
      await api("/api/certs/" + btn.dataset.id + "/deploy", { method: "POST", body: "{}" });
      alert("已下发");
    }
    if (btn.dataset.act === "renew") {
      try {
        await api("/api/certs/" + btn.dataset.id + "/renew", { method: "POST", body: "{}" });
        alert("续期成功");
      } catch (err) { alert(err.message); }
    }
    await refreshCerts();
  };
}
$("#btn-add-cert").onclick = async () => {
  await api("/api/certs", {
    method: "POST",
    body: JSON.stringify({
      name: $("#cert-name").value.trim(),
      domain: $("#cert-domain").value.trim(),
      cert_pem: $("#cert-pem").value,
      key_pem: $("#key-pem").value,
      provider: "manual",
    }),
  });
  await refreshCerts();
};
$("#btn-acme").onclick = async () => {
  const domain = $("#acme-domain").value.trim();
  if (!domain) return alert("填写域名");
  const btn = $("#btn-acme");
  btn.disabled = true;
  try {
    const r = await api("/api/certs/acme", {
      method: "POST",
      body: JSON.stringify({
        domain,
        email: $("#acme-email").value.trim(),
        challenge: $("#acme-challenge").value,
        dns_provider: $("#acme-dns").value,
        dns_api_token: $("#acme-token").value.trim(),
        dns_api_key: $("#acme-key").value.trim(),
        dns_api_secret: $("#acme-secret").value.trim(),
        server_id: $("#acme-server").value || "",
        staging: $("#acme-staging").checked,
        auto_renew: true,
      }),
    });
    alert("成功 id=" + r.id + " 到期 " + new Date(r.expire_at * 1000).toLocaleString());
    await refreshCerts();
  } catch (e) {
    alert(e.message);
    await refreshCerts();
  }
  btn.disabled = false;
};

$("#btn-load-ngx").onclick = async () => {
  const server_id = $("#ngx-server").value;
  const d = await api("/api/nginx?server_id=" + encodeURIComponent(server_id));
  $("#ngx-content").value = d.content || "";
};
$("#btn-save-ngx").onclick = async () => {
  await api("/api/nginx", {
    method: "PUT",
    body: JSON.stringify({ server_id: $("#ngx-server").value, content: $("#ngx-content").value }),
  });
  alert("已保存");
};

async function refreshTraffic() {
  const t = await api("/api/traffic");
  $("#traffic-box").textContent =
    `服务器累计 ↑ ${fmtBytes(t.server_up)}  ↓ ${fmtBytes(t.server_down)}\n用户已用 ${fmtBytes(t.user_used)}`;
  $("#traffic-daily").innerHTML = (t.daily || []).slice().reverse().map((d) => `
    <div class="item"><div>${escapeHtml(d.day)} · ${escapeHtml(String(d.server_id || "-").slice(0, 8))}
    <div class="meta">↑${fmtBytes(d.up)} ↓${fmtBytes(d.down)}</div></div></div>`).join("") || '<p class="muted">暂无</p>';
}

$("#btn-st-one").onclick = async () => {
  const r = await api("/api/speedtest", {
    method: "POST",
    body: JSON.stringify({ host: $("#st-host").value.trim(), port: Number($("#st-port").value), tls: $("#st-tls").checked }),
  });
  $("#speed-list").innerHTML = `<div class="item"><div><strong>${escapeHtml(r.target)}</strong>
    <div class="meta">TCP ${Number(r.tcp_ms).toFixed(2)} ms ${r.tls_ms ? "· TLS " + Number(r.tls_ms).toFixed(2) + " ms" : ""}
    ${r.error ? "· " + escapeHtml(r.error) : "· ok"}</div></div></div>`;
};
$("#btn-st-batch").onclick = async () => {
  const data = await api("/api/speedtest/batch", { method: "POST", body: "{}" });
  $("#speed-list").innerHTML = (data.results || []).map((x) => {
    const r = x.result || {};
    return `<div class="item"><div><strong>${escapeHtml(x.server)} / ${escapeHtml(x.tag)}</strong>
      <div class="meta">${escapeHtml(r.target || "")} · TCP ${r.tcp_ms != null ? Number(r.tcp_ms).toFixed(2) : "-"} ms
      ${r.error ? "· " + escapeHtml(r.error) : "· ok"}</div></div></div>`;
  }).join("") || '<p class="muted">无节点</p>';
};

async function refreshLinks() {
  const data = await api("/api/inbounds/links");
  $("#links-list").innerHTML = (data.links || []).map((l) => `
    <div class="item" style="align-items:flex-start"><div style="flex:1">
      <strong>${escapeHtml(l.name)}</strong> · ${escapeHtml(l.protocol)} ${escapeHtml(l.address)}:${l.port}
      <div class="mono" style="margin-top:.4rem">${escapeHtml(l.link || "(empty)")}</div>
    </div>
    <button class="small" data-copy="${escapeHtml(l.link || "")}">复制</button></div>`).join("") || '<p class="muted">暂无</p>';
  $("#links-list").onclick = async (e) => {
    const btn = e.target.closest("button[data-copy]");
    if (!btn) return;
    try {
      await navigator.clipboard.writeText(btn.dataset.copy);
      btn.textContent = "已复制";
      setTimeout(() => (btn.textContent = "复制"), 1200);
    } catch {
      prompt("复制:", btn.dataset.copy);
    }
  };
}
$("#btn-refresh-links").onclick = () => refreshLinks();

async function refreshSettings() {
  const data = await api("/api/settings");
  const s = data.settings || {};
  $("#set-site").value = s.site_name || "XPanel";
  let themeMode = localStorage.getItem("xpanel_theme_mode") || s.theme || "system";
  if (!THEME_MODES.includes(themeMode)) themeMode = "system";
  $("#set-theme").value = themeMode;
  $("#set-probe").value = s.probe_mode || "off";
  $("#set-acme-email").value = s.acme_email || "";
  $("#set-cf-token").value = s.cf_dns_api_token || "";
  $("#set-dns-key").value = s.dns_api_key || "";
  $("#set-dns-secret").value = s.dns_api_secret || "";
  $("#set-webhook").value = s.webhook_url || "";
  if (s.acme_email && $("#acme-email") && !$("#acme-email").value) $("#acme-email").value = s.acme_email;
}
$("#btn-save-set").onclick = async () => {
  let themeMode = $("#set-theme").value || "system";
  if (!THEME_MODES.includes(themeMode)) themeMode = "system";
  await api("/api/settings", {
    method: "PUT",
    body: JSON.stringify({
      site_name: $("#set-site").value.trim(),
      theme: themeMode,
      probe_mode: $("#set-probe").value,
      acme_email: $("#set-acme-email").value.trim(),
      cf_dns_api_token: $("#set-cf-token").value.trim(),
      dns_api_key: $("#set-dns-key").value.trim(),
      dns_api_secret: $("#set-dns-secret").value.trim(),
      webhook_url: $("#set-webhook").value.trim(),
    }),
  });
  setTheme(themeMode);
  alert("已保存");
};
$("#btn-backup").onclick = async () => {
  const res = await fetch("/api/backup/export", { headers: { Authorization: "Bearer " + state.token } });
  const blob = await res.blob();
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "xpanel-backup.json";
  a.click();
};

boot().catch((e) => {
  $("#auth-err").textContent = e.message;
  showAuth();
});
