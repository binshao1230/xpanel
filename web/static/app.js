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

const PAGE_META = {
  dash: { title: "仪表盘", sub: "总览在线节点、流量与趋势" },
  servers: { title: "服务器", sub: "主控 · Agent · 一键部署" },
  inbounds: { title: "入站节点", sub: "模板创建 · 侧滑编辑 · 一键复制" },
  outbounds: { title: "出站 / 路由", sub: "WARP、分流规则" },
  tunnels: { title: "中转隧道", sub: "端口转发" },
  plans: { title: "套餐", sub: "流量与有效期" },
  users: { title: "用户 / 续费", sub: "账号与配额" },
  invites: { title: "邀请码", sub: "注册绑定套餐" },
  nodes: { title: "外部节点", sub: "导入分享链接" },
  certs: { title: "证书 ACME", sub: "申请与下发" },
  nginx: { title: "Nginx", sub: "配置草稿" },
  traffic: { title: "流量", sub: "用量统计" },
  speed: { title: "测速", sub: "TCP / TLS 连通" },
  links: { title: "URI 分享", sub: "节点链接一览" },
  sub: { title: "我的订阅", sub: "一键复制到客户端" },
  settings: { title: "系统设置", sub: "站点与主题" },
};

function toast(msg, type = "info", ms = 2800) {
  const stack = $("#toast-stack");
  if (!stack) {
    showThemeToast(String(msg));
    return;
  }
  const el = document.createElement("div");
  el.className = "toast " + (type || "info");
  el.textContent = String(msg);
  stack.appendChild(el);
  setTimeout(() => {
    el.style.opacity = "0";
    el.style.transition = "opacity .25s ease";
    setTimeout(() => el.remove(), 280);
  }, ms);
}

function notify(msg, type = "ok") {
  const s = String(msg ?? "");
  if (s.includes("\n") || s.length > 120) showResult("提示", s);
  else toast(s, type);
}

async function copyText(text) {
  const t = String(text ?? "");
  if (!t) throw new Error("empty");
  try {
    await navigator.clipboard.writeText(t);
  } catch {
    const ta = document.createElement("textarea");
    ta.value = t;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
  }
  toast("已复制", "ok", 1600);
}

function uiConfirm(message, title = "确认操作") {
  return new Promise((resolve) => {
    const modal = $("#confirm-modal");
    if (!modal) {
      resolve(window.confirm(message));
      return;
    }
    $("#confirm-title").textContent = title;
    $("#confirm-msg").textContent = message;
    modal.classList.remove("hidden");
    const ok = $("#confirm-ok");
    const cancel = $("#confirm-cancel");
    const done = (v) => {
      modal.classList.add("hidden");
      ok.onclick = null;
      cancel.onclick = null;
      resolve(v);
    };
    ok.onclick = () => done(true);
    cancel.onclick = () => done(false);
  });
}

function showResult(title, body) {
  if ($("#result-title")) $("#result-title").textContent = title || "完成";
  if ($("#result-body")) $("#result-body").textContent = body || "";
  $("#result-modal")?.classList.remove("hidden");
}
function closeResult() {
  $("#result-modal")?.classList.add("hidden");
}
function setSideOpen(open) {
  $("#view-main")?.classList.toggle("side-open", !!open);
}
function openNodeDrawer() {
  $("#in-drawer")?.classList.remove("hidden");
  document.body.style.overflow = "hidden";
}
function closeNodeDrawer() {
  $("#in-drawer")?.classList.add("hidden");
  document.body.style.overflow = "";
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
$("#modal-close") && ($("#modal-close").onclick = () => $("#modal").classList.add("hidden"));
$("#modal-close-2") && ($("#modal-close-2").onclick = () => $("#modal").classList.add("hidden"));
$("#modal-copy") && ($("#modal-copy").onclick = () => copyText($("#install-cmd")?.textContent || "").catch(() => {}));
$("#result-close") && ($("#result-close").onclick = closeResult);
$("#result-close-2") && ($("#result-close-2").onclick = closeResult);
$("#result-copy") && ($("#result-copy").onclick = () => copyText($("#result-body")?.textContent || "").catch(() => {}));
$("#btn-side-toggle") && ($("#btn-side-toggle").onclick = () => {
  const open = !($("#view-main")?.classList.contains("side-open"));
  setSideOpen(open);
});
$("#side-mask") && ($("#side-mask").onclick = () => setSideOpen(false));
$("#btn-global-refresh") && ($("#btn-global-refresh").onclick = () => {
  const active = document.querySelector("#nav .nav.active");
  switchTab(active?.dataset.tab || "dash");
  toast("已刷新", "ok", 1200);
});
$$("#dash-quick .qa-card").forEach((b) => {
  b.onclick = () => {
    const go = b.dataset.go;
    if (go) switchTab(go);
    if (go === "inbounds") setTimeout(() => { resetInboundForm(); openNodeDrawer(); }, 80);
  };
});
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-copy-el]");
  if (!btn) return;
  const el = document.getElementById(btn.dataset.copyEl);
  if (el) copyText(el.textContent).catch((err) => toast(err.message, "err"));
});

function switchTab(name) {
  $$("#nav .nav").forEach((b) => b.classList.toggle("active", b.dataset.tab === name));
  $$(".tab").forEach((t) => t.classList.add("hidden"));
  const el = $("#tab-" + name);
  if (el) el.classList.remove("hidden");
  const meta = PAGE_META[name] || { title: name, sub: "" };
  if ($("#page-title")) $("#page-title").textContent = meta.title;
  if ($("#page-sub")) $("#page-sub").textContent = meta.sub;
  setSideOpen(false);
  state.currentTab = name;
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
  if (loaders[name]) loaders[name]().catch((e) => toast(e.message, "err"));
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
    sel.innerHTML = head + state.servers.map((s) => {
      const domain = s.domain || s.public_ip || "";
      return `<option value="${s.id}" data-domain="${escapeHtml(domain)}">${escapeHtml(s.name)}</option>`;
    }).join("");
  }
}
async function fillCertSelect() {
  const data = await api("/api/certs");
  state.certs = data.certificates || [];
  const sel = $("#in-cert");
  if (!sel) return;
  sel.innerHTML = '<option value="0">无证书 / Reality 自管</option>' +
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
  }).join("") || `<div class="empty-state"><h3>还没有服务器</h3><p>添加一台并安装 Agent 即可下发节点</p></div>`;
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!(await uiConfirm("删除后 Agent 将无法再连接，确定删除该服务器？", "删除服务器"))) return;
      await api("/api/servers/" + id, { method: "DELETE" });
      toast("已删除服务器", "ok");
      await refreshServers();
    }
    if (btn.dataset.act === "bump") {
      await api("/api/servers/" + id + "/bump-config", { method: "POST", body: "{}" });
      toast("已触发配置下发", "ok");
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

// —— 节点编辑：可折叠 + 模板选择 + 自定义 ——
const NODE_TEMPLATES = {
  "vless-reality": {
    proto: "vless", port: 443, network: "tcp", security: "reality",
    flow: "xtls-rprx-vision", path: "", host: "",
    sni: "www.microsoft.com", dest: "www.microsoft.com:443",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
  "vless-ws-tls": {
    proto: "vless", port: 443, network: "ws", security: "tls",
    flow: "", path: "/ray", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "vless-grpc-tls": {
    proto: "vless", port: 443, network: "grpc", security: "tls",
    flow: "", path: "GunService", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2", method: "aes-256-gcm",
  },
  "vless-xhttp-tls": {
    proto: "vless", port: 443, network: "splithttp", security: "tls",
    flow: "", path: "/xhttp", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "vmess-ws-tls": {
    proto: "vmess", port: 443, network: "ws", security: "tls",
    flow: "", path: "/vmess", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "trojan-tcp-tls": {
    proto: "trojan", port: 443, network: "tcp", security: "tls",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "trojan-ws-tls": {
    proto: "trojan", port: 443, network: "ws", security: "tls",
    flow: "", path: "/trojan", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "ss-tcp": {
    proto: "shadowsocks", port: 8388, network: "tcp", security: "none",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
};

function serverDomainHint() {
  const sel = $("#in-server");
  if (!sel || !sel.selectedOptions || !sel.selectedOptions[0]) return "";
  // option text often "name (id)" — domain not always available; try data-domain
  return sel.selectedOptions[0].dataset.domain || "";
}

/** 模板 select + 自定义 input：选 __custom__ 显示 input，其它隐藏 */
function bindTplCustom(selectId, wrapId, inputId, opts = {}) {
  const sel = $(selectId);
  const wrap = wrapId ? $(wrapId) : null;
  const input = inputId ? $(inputId) : null;
  if (!sel) return;
  const sync = () => {
    const v = sel.value;
    const isCustom = v === "__custom__";
    if (wrap) wrap.classList.toggle("hidden", !isCustom);
    if (isCustom && input && opts.focus) input.focus();
  };
  sel.onchange = () => {
    if (sel.value !== "__custom__" && input && sel.value !== "__domain__" && sel.value !== "__keep__") {
      // keep input in sync for non-custom for collect
      if (input && sel.value !== "") input.value = sel.value;
    }
    if (sel.value === "__domain__" && input) {
      input.value = serverDomainHint();
    }
    sync();
    if (typeof opts.onChange === "function") opts.onChange(sel.value);
  };
  sync();
}

function setTplValue(selectId, wrapId, inputId, value) {
  const sel = $(selectId);
  const wrap = wrapId ? $(wrapId) : null;
  const input = inputId ? $(inputId) : null;
  if (!sel) return;
  const val = value == null ? "" : String(value);
  const opts = [...sel.options].map((o) => o.value);
  if (val === "" && opts.includes("")) {
    sel.value = "";
    if (input) input.value = "";
  } else if (opts.includes(val)) {
    sel.value = val;
    if (input) input.value = val;
  } else if (val !== "" && opts.includes("__custom__")) {
    sel.value = "__custom__";
    if (input) input.value = val;
  } else if (opts.includes(val)) {
    sel.value = val;
  } else {
    // fallback first option
    if (opts.includes("__custom__") && val) {
      sel.value = "__custom__";
      if (input) input.value = val;
    }
  }
  if (wrap) wrap.classList.toggle("hidden", sel.value !== "__custom__");
}

function getTplValue(selectId, inputId) {
  const sel = $(selectId);
  if (!sel) return inputId && $(inputId) ? $(inputId).value.trim() : "";
  const v = sel.value;
  if (v === "__custom__") return inputId && $(inputId) ? $(inputId).value.trim() : "";
  if (v === "__domain__") return serverDomainHint() || (inputId && $(inputId) ? $(inputId).value.trim() : "");
  if (v === "__keep__") return "__keep__";
  return v;
}

function setSelectOrCustomSimple(selectId, customInputId, wrapId, value) {
  // for selects that embed __custom__ and use separate custom input (fp, alpn, flow)
  const sel = $(selectId);
  const input = customInputId ? $(customInputId) : null;
  const wrap = wrapId ? $(wrapId) : null;
  if (!sel) return;
  const val = value == null ? "" : String(value);
  const opts = [...sel.options].map((o) => o.value);
  if (opts.includes(val)) {
    sel.value = val;
  } else if (val && opts.includes("__custom__")) {
    sel.value = "__custom__";
    if (input) input.value = val;
  } else {
    sel.value = opts[0] || "";
  }
  if (wrap) wrap.classList.toggle("hidden", sel.value !== "__custom__");
}

function getSelectOrCustomSimple(selectId, customInputId) {
  const sel = $(selectId);
  if (!sel) return "";
  if (sel.value === "__custom__") {
    return customInputId && $(customInputId) ? $(customInputId).value.trim() : "";
  }
  return sel.value;
}

function parseStreamForm(stream) {
  const s = stream || {};
  const network = s.network || "tcp";
  const security = s.security || "none";
  let path = "", host = "", sni = "", dest = "", pbk = "", priv = "", sid = "", fp = "chrome", alpn = "";
  if (s.wsSettings) {
    path = s.wsSettings.path || "";
    host = (s.wsSettings.headers && s.wsSettings.headers.Host) || "";
  }
  if (s.grpcSettings) path = s.grpcSettings.serviceName || path;
  if (s.httpSettings) {
    path = s.httpSettings.path || path;
    if (Array.isArray(s.httpSettings.host) && s.httpSettings.host[0]) host = s.httpSettings.host[0];
  }
  if (s.httpupgradeSettings) {
    path = s.httpupgradeSettings.path || path;
    host = s.httpupgradeSettings.host || host;
  }
  if (s.splithttpSettings) {
    path = s.splithttpSettings.path || path;
    host = s.splithttpSettings.host || host;
  }
  if (s.tlsSettings) {
    sni = s.tlsSettings.serverName || "";
    fp = s.tlsSettings.fingerprint || fp;
    if (Array.isArray(s.tlsSettings.alpn)) alpn = s.tlsSettings.alpn.join(",");
  }
  if (s.realitySettings) {
    dest = s.realitySettings.dest || "";
    priv = s.realitySettings.privateKey || "";
    fp = s.realitySettings.fingerprint || fp;
    if (Array.isArray(s.realitySettings.serverNames) && s.realitySettings.serverNames[0]) {
      sni = s.realitySettings.serverNames[0];
    }
    if (Array.isArray(s.realitySettings.shortIds) && s.realitySettings.shortIds[0]) {
      sid = s.realitySettings.shortIds[0];
    }
  }
  return { network, security, path, host, sni, dest, pbk, priv, sid, fp, alpn };
}

function parseSettingsForm(settings) {
  const st = settings || {};
  let uuid = "", flow = "", password = "", method = "aes-256-gcm";
  if (Array.isArray(st.clients) && st.clients[0]) {
    uuid = st.clients[0].id || "";
    flow = st.clients[0].flow || "";
    password = st.clients[0].password || password;
  }
  if (st.password) password = st.password;
  if (st.method) method = st.method;
  return { uuid, flow, password, method, pbk: (st.xpanelMeta && st.xpanelMeta.publicKey) || "" };
}

function setFoldsOpen(open) {
  $$("#in-editor details.fold").forEach((d) => {
    d.open = !!open;
  });
}

function applyNodeTemplate(id) {
  if (!id || id === "custom") return;
  const t = NODE_TEMPLATES[id];
  if (!t) return;
  $("#in-proto").value = t.proto;
  setTplValue("#in-port-tpl", "#wrap-in-port", "#in-port", String(t.port));
  $("#in-port").value = t.port;
  $("#in-network").value = t.network;
  $("#in-security").value = t.security;
  setTplValue("#in-path-tpl", "#wrap-in-path", "#in-path", t.path || "");
  setTplValue("#in-host-tpl", "#wrap-in-host", "#in-host", t.host || "");
  setTplValue("#in-sni-tpl", "#wrap-in-sni", "#in-sni", t.sni || "");
  setTplValue("#in-dest-tpl", "#wrap-in-dest", "#in-dest", t.dest || "");
  setSelectOrCustomSimple("#in-fp", "#in-fp-custom", "#wrap-in-fp-custom", t.fp || "chrome");
  setSelectOrCustomSimple("#in-alpn", "#in-alpn-custom", "#wrap-in-alpn-custom", t.alpn || "");
  setSelectOrCustomSimple("#in-flow", "#in-flow-custom", "#wrap-in-flow-custom", t.flow || "");
  $("#in-method").value = t.method || "aes-256-gcm";
  // 密钥默认自动
  setTplValue("#in-uuid-tpl", "#wrap-in-uuid", "#in-uuid", "");
  setTplValue("#in-password-tpl", "#wrap-in-password", "#in-password", "");
  if ($("#in-reality-key-tpl")) {
    $("#in-reality-key-tpl").value = "";
    ["#wrap-in-pbk", "#wrap-in-priv", "#wrap-in-sid"].forEach((id) => {
      const el = $(id);
      if (el) el.classList.add("hidden");
    });
    $("#in-pbk").value = "";
    $("#in-priv").value = "";
    $("#in-sid").value = "";
  }
  if (!$("#in-tag").value.trim()) {
    $("#in-tag").value = `${t.proto}-${t.port}`;
  }
  if ($("#in-use-json")) $("#in-use-json").checked = false;
  $("#in-editor-hint").textContent = `已套用模板，可继续微调；密钥默认自动生成。`;
}

function resetInboundForm() {
  $("#in-id").value = "";
  $("#in-editor-title").textContent = "新建节点";
  if ($("#in-template")) $("#in-template").value = "";
  setTplValue("#in-port-tpl", "#wrap-in-port", "#in-port", "443");
  $("#in-port").value = 443;
  $("#in-tag").value = "";
  $("#in-share-name").value = "";
  setTplValue("#in-remark-tpl", "#wrap-in-remark", "#in-remark", "");
  setTplValue("#in-mult-tpl", "#wrap-in-mult", "#in-mult", "1");
  $("#in-mult").value = 1;
  $("#in-cert").value = "0";
  $("#in-network").value = "tcp";
  $("#in-security").value = "none";
  setTplValue("#in-path-tpl", "#wrap-in-path", "#in-path", "");
  setTplValue("#in-host-tpl", "#wrap-in-host", "#in-host", "");
  setTplValue("#in-sni-tpl", "#wrap-in-sni", "#in-sni", "");
  setTplValue("#in-dest-tpl", "#wrap-in-dest", "#in-dest", "");
  setSelectOrCustomSimple("#in-fp", "#in-fp-custom", "#wrap-in-fp-custom", "chrome");
  setSelectOrCustomSimple("#in-alpn", "#in-alpn-custom", "#wrap-in-alpn-custom", "");
  setTplValue("#in-uuid-tpl", "#wrap-in-uuid", "#in-uuid", "");
  setSelectOrCustomSimple("#in-flow", "#in-flow-custom", "#wrap-in-flow-custom", "");
  setTplValue("#in-password-tpl", "#wrap-in-password", "#in-password", "");
  $("#in-method").value = "aes-256-gcm";
  $("#in-enabled").value = "1";
  if ($("#in-reality-key-tpl")) $("#in-reality-key-tpl").value = "";
  $("#in-pbk").value = "";
  $("#in-priv").value = "";
  $("#in-sid").value = "";
  ["#wrap-in-pbk", "#wrap-in-priv", "#wrap-in-sid"].forEach((id) => {
    const el = $(id);
    if (el) el.classList.add("hidden");
  });
  $("#in-settings-json").value = "";
  $("#in-stream-json").value = "";
  if ($("#in-use-json")) $("#in-use-json").checked = false;
  $("#in-editor-hint").textContent = "先选模板，再按需微调。各分区可折叠。";
  $$("#in-editor details.fold").forEach((d, i) => {
    d.open = i < 3;
  });
}

function fillInboundForm(inb) {
  $("#in-id").value = inb.id || "";
  $("#in-editor-title").textContent = inb.id ? `编辑节点 #${inb.id}` : "新建节点";
  if (inb.server_id) $("#in-server").value = inb.server_id;
  $("#in-proto").value = inb.protocol || "vless";
  setTplValue("#in-port-tpl", "#wrap-in-port", "#in-port", String(inb.port || 443));
  $("#in-port").value = inb.port || 443;
  $("#in-tag").value = inb.tag || "";
  $("#in-share-name").value = inb.share_name || "";
  setTplValue("#in-remark-tpl", "#wrap-in-remark", "#in-remark", inb.remark || "");
  setTplValue("#in-mult-tpl", "#wrap-in-mult", "#in-mult", String(inb.multiplier != null ? inb.multiplier : 1));
  $("#in-mult").value = inb.multiplier != null ? inb.multiplier : 1;
  $("#in-cert").value = String(inb.cert_id || 0);
  $("#in-enabled").value = inb.enabled === false ? "0" : "1";

  let settings = {};
  let stream = {};
  try { settings = typeof inb.settings_json === "string" ? JSON.parse(inb.settings_json || "{}") : (inb.settings || {}); } catch { settings = {}; }
  try { stream = typeof inb.stream_json === "string" ? JSON.parse(inb.stream_json || "{}") : (inb.stream || {}); } catch { stream = {}; }

  const sf = parseSettingsForm(settings);
  const tf = parseStreamForm(stream);
  $("#in-network").value = tf.network || "tcp";
  $("#in-security").value = tf.security || "none";
  setTplValue("#in-path-tpl", "#wrap-in-path", "#in-path", tf.path || "");
  setTplValue("#in-host-tpl", "#wrap-in-host", "#in-host", tf.host || "");
  setTplValue("#in-sni-tpl", "#wrap-in-sni", "#in-sni", tf.sni || "");
  setTplValue("#in-dest-tpl", "#wrap-in-dest", "#in-dest", tf.dest || "");
  setSelectOrCustomSimple("#in-fp", "#in-fp-custom", "#wrap-in-fp-custom", tf.fp || "chrome");
  setSelectOrCustomSimple("#in-alpn", "#in-alpn-custom", "#wrap-in-alpn-custom", tf.alpn || "");
  setSelectOrCustomSimple("#in-flow", "#in-flow-custom", "#wrap-in-flow-custom", sf.flow || "");
  $("#in-method").value = sf.method || "aes-256-gcm";

  if (sf.uuid) {
    setTplValue("#in-uuid-tpl", "#wrap-in-uuid", "#in-uuid", sf.uuid);
  } else {
    setTplValue("#in-uuid-tpl", "#wrap-in-uuid", "#in-uuid", "");
  }
  if (sf.password) {
    setTplValue("#in-password-tpl", "#wrap-in-password", "#in-password", sf.password);
  } else {
    setTplValue("#in-password-tpl", "#wrap-in-password", "#in-password", "");
  }

  // Reality keys: keep by default when editing
  const pbk = sf.pbk || "";
  const priv = tf.priv || "";
  const sid = tf.sid || "";
  $("#in-pbk").value = pbk;
  $("#in-priv").value = priv;
  $("#in-sid").value = sid;
  if ($("#in-reality-key-tpl")) {
    if (priv || pbk) {
      $("#in-reality-key-tpl").value = "__keep__";
      ["#wrap-in-pbk", "#wrap-in-priv", "#wrap-in-sid"].forEach((id) => {
        const el = $(id);
        if (el) el.classList.add("hidden");
      });
    } else {
      $("#in-reality-key-tpl").value = "";
    }
  }

  $("#in-settings-json").value = JSON.stringify(settings, null, 2);
  $("#in-stream-json").value = JSON.stringify(stream, null, 2);
  if ($("#in-use-json")) $("#in-use-json").checked = false;
  if ($("#in-template")) $("#in-template").value = "custom";
  $("#in-editor-hint").textContent = "正在编辑。改模板/下拉即可；密钥区默认折叠。完全自定义请开「高级 JSON」。";
  $$("#in-editor details.fold").forEach((d, i) => {
    d.open = i < 3;
  });
  $("#in-editor")?.scrollIntoView({ behavior: "smooth", block: "start" });
}

function collectInboundPayload() {
  const cert_id = Number($("#in-cert").value) || 0;
  const port = Number(getTplValue("#in-port-tpl", "#in-port")) || Number($("#in-port").value) || 0;
  const multRaw = getTplValue("#in-mult-tpl", "#in-mult");
  const mult = Number(multRaw);
  const path = getTplValue("#in-path-tpl", "#in-path");
  const host = getTplValue("#in-host-tpl", "#in-host");
  const sni = getTplValue("#in-sni-tpl", "#in-sni");
  const dest = getTplValue("#in-dest-tpl", "#in-dest");
  const fp = getSelectOrCustomSimple("#in-fp", "#in-fp-custom") || "chrome";
  const alpn = getSelectOrCustomSimple("#in-alpn", "#in-alpn-custom");
  const flow = getSelectOrCustomSimple("#in-flow", "#in-flow-custom");
  const uuidTpl = getTplValue("#in-uuid-tpl", "#in-uuid");
  const passwordTpl = getTplValue("#in-password-tpl", "#in-password");
  const remark = getTplValue("#in-remark-tpl", "#in-remark");

  let public_key = "";
  let private_key = "";
  let short_id = "";
  const rkey = $("#in-reality-key-tpl") ? $("#in-reality-key-tpl").value : "";
  if (rkey === "__custom__") {
    public_key = ($("#in-pbk").value || "").trim();
    private_key = ($("#in-priv").value || "").trim();
    short_id = ($("#in-sid").value || "").trim();
  } else if (rkey === "__keep__") {
    // send existing keys so server keeps them when form rebuilds
    public_key = ($("#in-pbk").value || "").trim();
    private_key = ($("#in-priv").value || "").trim();
    short_id = ($("#in-sid").value || "").trim();
  }
  // rkey "" → auto: leave keys empty for server generate

  const payload = {
    server_id: $("#in-server").value,
    protocol: $("#in-proto").value,
    port,
    tag: $("#in-tag").value.trim(),
    share_name: $("#in-share-name").value.trim(),
    remark,
    multiplier: Number.isFinite(mult) && mult > 0 ? mult : 1,
    cert_id,
    enable_tls: cert_id > 0 && $("#in-security").value === "tls",
    network: $("#in-network").value,
    security: $("#in-security").value,
    path,
    host,
    service_name: path,
    sni,
    dest,
    public_key,
    private_key,
    short_id,
    fingerprint: fp,
    alpn,
    uuid: uuidTpl,
    flow,
    password: passwordTpl,
    method: $("#in-method").value,
    enabled: $("#in-enabled").value !== "0",
  };

  const useJSON = $("#in-use-json") && $("#in-use-json").checked;
  if (useJSON) {
    const sj = $("#in-settings-json").value.trim();
    const stj = $("#in-stream-json").value.trim();
    if (sj) {
      try { JSON.parse(sj); } catch (e) { throw new Error("settings_json 不是合法 JSON: " + e.message); }
      payload.settings_json = sj;
    }
    if (stj) {
      try { JSON.parse(stj); } catch (e) { throw new Error("stream_json 不是合法 JSON: " + e.message); }
      payload.stream_json = stj;
    }
  }
  return payload;
}

state.inboundsCache = [];
state.inboundLinks = {};

function matchInboundFilter(inb, stream) {
  const q = ($("#in-search")?.value || "").trim().toLowerCase();
  const protoF = $("#in-filter-proto")?.value || "";
  const secF = $("#in-filter-sec")?.value || "";
  const net = stream.network || "tcp";
  const sec = stream.security || "none";
  if (protoF && inb.protocol !== protoF) return false;
  if (secF && sec !== secF) return false;
  if (!q) return true;
  const hay = [inb.share_name, inb.tag, inb.protocol, inb.remark, inb.port, net, sec].join(" ").toLowerCase();
  return hay.includes(q);
}

function renderInboundList() {
  const all = state.inboundsCache || [];
  const list = all.filter((inb) => {
    let stream = {};
    try { stream = JSON.parse(inb.stream_json || "{}"); } catch { /* */ }
    return matchInboundFilter(inb, stream);
  });
  if ($("#in-count")) $("#in-count").textContent = `共 ${list.length} / ${all.length}`;
  const box = $("#inbound-list");
  if (!box) return;
  if (!list.length) {
    box.innerHTML = `<div class="empty-state"><h3>${all.length ? "没有匹配的节点" : "还没有节点"}</h3>
      <p>${all.length ? "试试清空搜索或筛选" : "用快捷模板或「新建节点」创建"}</p>
      ${!all.length ? '<button type="button" class="primary" id="btn-empty-new">+ 新建节点</button>' : ""}</div>`;
    const bn = $("#btn-empty-new");
    if (bn) bn.onclick = () => { resetInboundForm(); openNodeDrawer(); };
    return;
  }
  box.innerHTML = list.map((inb) => {
    let stream = {};
    try { stream = JSON.parse(inb.stream_json || "{}"); } catch { /* */ }
    const net = stream.network || "tcp";
    const sec = stream.security || "none";
    const en = inb.enabled !== false;
    const link = state.inboundLinks[inb.id] || "";
    return `<div class="item">
      <div class="item-main">
        <div class="item-title">
          <strong>${escapeHtml(inb.share_name || inb.tag)}</strong>
          <span class="chip">${escapeHtml(inb.protocol)}</span>
          <span class="chip">${escapeHtml(net)}/${escapeHtml(sec)}</span>
          <span class="chip">:${inb.port}</span>
          ${en ? '<span class="chip on">启用</span>' : '<span class="chip off">禁用</span>'}
        </div>
        <div class="meta">tag ${escapeHtml(inb.tag)} · x${inb.multiplier || 1}${inb.remark ? " · " + escapeHtml(inb.remark) : ""}</div>
      </div>
      <div class="item-actions row">
        <button class="small" data-act="edit" data-id="${inb.id}">编辑</button>
        <button class="small" data-act="copy" data-id="${inb.id}" ${link ? "" : "disabled"}>复制链接</button>
        <button class="small" data-act="clone" data-id="${inb.id}">克隆</button>
        <button class="small" data-act="toggle" data-id="${inb.id}" data-en="${en ? 0 : 1}">${en ? "禁用" : "启用"}</button>
        <button class="small danger" data-act="del" data-id="${inb.id}">删除</button>
      </div>
    </div>`;
  }).join("");
}

async function refreshInbounds() {
  const [data, linksData] = await Promise.all([
    api("/api/inbounds"),
    api("/api/inbounds/links").catch(() => ({ links: [] })),
  ]);
  state.inboundsCache = data.inbounds || [];
  state.inboundLinks = {};
  (linksData.links || []).forEach((l) => { if (l.id != null) state.inboundLinks[l.id] = l.link || ""; });
  renderInboundList();
  const box = $("#inbound-list");
  if (!box) return;
  box.onclick = async (e) => {
    const btn = e.target.closest("button[data-act]");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!(await uiConfirm("删除后配置将从 Agent 移除，确定？", "删除节点"))) return;
      try {
        await api("/api/inbounds/" + id, { method: "DELETE" });
        toast("节点已删除", "ok");
        await refreshInbounds();
      } catch (err) { toast(err.message, "err"); }
      return;
    }
    if (btn.dataset.act === "toggle") {
      try {
        await api("/api/inbounds/" + id, { method: "PUT", body: JSON.stringify({ enabled: btn.dataset.en === "1" }) });
        toast(btn.dataset.en === "1" ? "已启用" : "已禁用", "ok");
      } catch (err) { toast(err.message, "err"); }
      await refreshInbounds();
      return;
    }
    if (btn.dataset.act === "copy") {
      const link = state.inboundLinks[id];
      if (!link) return toast("暂无分享链接", "warn");
      try { await copyText(link); } catch (err) { toast(err.message, "err"); }
      return;
    }
    if (btn.dataset.act === "clone") {
      try {
        const r = await api("/api/inbounds/" + id);
        const inb = r.inbound || r;
        fillInboundForm(inb);
        $("#in-id").value = "";
        $("#in-editor-title").textContent = "克隆节点（新建）";
        $("#in-tag").value = (inb.tag || "node") + "-copy";
        if ($("#in-share-name")) $("#in-share-name").value = (inb.share_name || inb.tag || "") + " 副本";
        const p = (Number(inb.port) || 443) + 1;
        setTplValue("#in-port-tpl", "#wrap-in-port", "#in-port", String(p));
        $("#in-port").value = p;
        openNodeDrawer();
        toast("已填入克隆内容", "info");
      } catch (err) { toast(err.message, "err"); }
      return;
    }
    if (btn.dataset.act === "edit") {
      try {
        const r = await api("/api/inbounds/" + id);
        fillInboundForm(r.inbound || r);
        openNodeDrawer();
      } catch (err) { toast(err.message, "err"); }
    }
  };
}

function initInboundEditorUI() {
  // template + custom field bindings
  bindTplCustom("#in-port-tpl", "#wrap-in-port", "#in-port");
  bindTplCustom("#in-mult-tpl", "#wrap-in-mult", "#in-mult");
  bindTplCustom("#in-remark-tpl", "#wrap-in-remark", "#in-remark");
  bindTplCustom("#in-path-tpl", "#wrap-in-path", "#in-path");
  bindTplCustom("#in-host-tpl", "#wrap-in-host", "#in-host");
  bindTplCustom("#in-sni-tpl", "#wrap-in-sni", "#in-sni");
  bindTplCustom("#in-dest-tpl", "#wrap-in-dest", "#in-dest");
  bindTplCustom("#in-uuid-tpl", "#wrap-in-uuid", "#in-uuid");
  bindTplCustom("#in-password-tpl", "#wrap-in-password", "#in-password");
  bindTplCustom("#in-fp", "#wrap-in-fp-custom", "#in-fp-custom");
  bindTplCustom("#in-alpn", "#wrap-in-alpn-custom", "#in-alpn-custom");
  bindTplCustom("#in-flow", "#wrap-in-flow-custom", "#in-flow-custom");

  const rkey = $("#in-reality-key-tpl");
  if (rkey) {
    rkey.onchange = () => {
      const show = rkey.value === "__custom__";
      ["#wrap-in-pbk", "#wrap-in-priv", "#wrap-in-sid"].forEach((id) => {
        const el = $(id);
        if (el) el.classList.toggle("hidden", !show);
      });
    };
  }

  if ($("#in-template")) {
    $("#in-template").onchange = () => applyNodeTemplate($("#in-template").value);
  }

  if ($("#btn-in-close")) $("#btn-in-close").onclick = () => closeNodeDrawer();
  if ($("#in-drawer-backdrop")) $("#in-drawer-backdrop").onclick = () => closeNodeDrawer();
  if ($("#btn-in-fold-all")) $("#btn-in-fold-all").onclick = () => setFoldsOpen(false);
  if ($("#btn-in-expand-all")) $("#btn-in-expand-all").onclick = () => setFoldsOpen(true);

  ["#in-search", "#in-filter-proto", "#in-filter-sec"].forEach((sel) => {
    const el = $(sel);
    if (!el) return;
    el.oninput = el.onchange = () => renderInboundList();
  });

  $$("#in-quick-tpl .chip-btn").forEach((b) => {
    b.onclick = async () => {
      await fillServerSelects();
      await fillCertSelect();
      resetInboundForm();
      if ($("#in-template")) $("#in-template").value = b.dataset.tpl;
      applyNodeTemplate(b.dataset.tpl);
      openNodeDrawer();
    };
  });

  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if ($("#in-drawer") && !$("#in-drawer").classList.contains("hidden")) closeNodeDrawer();
    else if ($("#result-modal") && !$("#result-modal").classList.contains("hidden")) closeResult();
    else if ($("#modal") && !$("#modal").classList.contains("hidden")) $("#modal").classList.add("hidden");
  });
}

initInboundEditorUI();

$("#btn-in-new") && ($("#btn-in-new").onclick = async () => {
  await fillServerSelects();
  await fillCertSelect();
  resetInboundForm();
  openNodeDrawer();
});
$("#btn-in-reset") && ($("#btn-in-reset").onclick = () => resetInboundForm());
$("#btn-in-fill-json") && ($("#btn-in-fill-json").onclick = () => {
  const p = collectInboundPayload();
  const settings = { decryption: p.protocol === "vless" ? "none" : undefined };
  if (p.protocol === "vless" || p.protocol === "vmess") {
    settings.clients = [{ id: p.uuid || "(auto)", email: "default@xpanel", flow: p.flow || "" }];
    if (p.protocol !== "vless") delete settings.decryption;
  } else if (p.protocol === "trojan") {
    settings.clients = [{ password: p.password || "(auto)", email: "trojan@xpanel" }];
  } else {
    settings.method = p.method;
    settings.password = p.password || "(auto)";
    settings.network = "tcp,udp";
  }
  const stream = { network: p.network, security: p.security };
  if (p.network === "ws") stream.wsSettings = { path: p.path || "/", headers: p.host ? { Host: p.host } : undefined };
  if (p.network === "grpc") stream.grpcSettings = { serviceName: p.path || "GunService" };
  if (p.security === "tls") stream.tlsSettings = { serverName: p.sni || "", fingerprint: p.fingerprint };
  if (p.security === "reality") {
    stream.realitySettings = {
      dest: p.dest || "www.microsoft.com:443",
      serverNames: [p.sni || "www.microsoft.com"],
      privateKey: p.private_key || "(auto)",
      shortIds: [p.short_id || "(auto)", ""],
      fingerprint: p.fingerprint || "chrome",
    };
  }
  $("#in-settings-json").value = JSON.stringify(settings, null, 2);
  $("#in-stream-json").value = JSON.stringify(stream, null, 2);
  toast("已生成 JSON 预览", "ok", 1500);
});

$("#btn-in-save") && ($("#btn-in-save").onclick = async () => {
  const saveBtn = $("#btn-in-save");
  try {
    const id = ($("#in-id").value || "").trim();
    const payload = collectInboundPayload();
    if (!payload.server_id) return toast("请选择服务器", "warn");
    if (!payload.port) return toast("端口无效", "warn");
    if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = "保存中…"; }
    let r;
    if (id) {
      r = await api("/api/inbounds/" + id, { method: "PUT", body: JSON.stringify(payload) });
    } else {
      r = await api("/api/inbounds", { method: "POST", body: JSON.stringify(payload) });
    }
    let msg = (id ? "已更新节点 #" : "已创建节点 #") + (r.id || id);
    if (r.share_link) msg += "\n\n分享链接:\n" + r.share_link;
    if (r.settings?.password) msg += "\n\nSS 密码: " + r.settings.password;
    if (r.settings?.clients?.[0]?.password) msg += "\n\nTrojan 密码: " + r.settings.clients[0].password;
    if (r.settings?.clients?.[0]?.id) msg += "\n\nUUID: " + r.settings.clients[0].id;
    if (r.settings?.xpanelMeta?.publicKey) msg += "\n\nReality publicKey:\n" + r.settings.xpanelMeta.publicKey;
    toast(id ? "节点已更新并下发" : "节点已创建并下发", "ok");
    showResult(id ? "更新成功" : "创建成功", msg);
    resetInboundForm();
    closeNodeDrawer();
    await refreshInbounds();
  } catch (e) {
    toast(e.message, "err");
  } finally {
    if (saveBtn) { saveBtn.disabled = false; saveBtn.textContent = "保存并下发"; }
  }
});

$("#btn-reality") && ($("#btn-reality").onclick = async () => {
  await fillServerSelects();
  await fillCertSelect();
  let server_id = $("#in-server")?.value;
  if (!server_id && state.servers?.[0]) {
    server_id = state.servers[0].id;
    if ($("#in-server")) $("#in-server").value = server_id;
  }
  if (!server_id) return toast("请先添加并选择服务器", "warn");
  try {
    const r = await api("/api/inbounds/quick-reality", {
      method: "POST",
      body: JSON.stringify({
        server_id,
        port: Number(getTplValue("#in-port-tpl", "#in-port")) || 443,
        dest: getTplValue("#in-dest-tpl", "#in-dest") || undefined,
        sni: getTplValue("#in-sni-tpl", "#in-sni") || undefined,
        flow: getSelectOrCustomSimple("#in-flow", "#in-flow-custom") || "xtls-rprx-vision",
        name: $("#in-tag")?.value.trim() || undefined,
      }),
    });
    toast("Reality 节点已创建", "ok");
    showResult("Reality 已创建", `PublicKey:\n${r.public_key}\n\n分享链接:\n${r.share_link || ""}`);
    await refreshInbounds();
  } catch (e) {
    toast(e.message, "err");
  }
});

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
        toast("启用失败: " + err.message, "err");
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
  toast(r.note || "ok", "ok");
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
      if (!(await uiConfirm("确定删除该用户？", "删除用户"))) return;
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
  toast("已生成邀请码", "ok");
  if ((r.codes || []).length) showResult("邀请码", (r.codes || []).join("\n"));
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
  toast("导入 " + r.imported + " 条", "ok");
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
      toast("已下发", "ok");
    }
    if (btn.dataset.act === "renew") {
      try {
        await api("/api/certs/" + btn.dataset.id + "/renew", { method: "POST", body: "{}" });
        toast("续期成功", "ok");
      } catch (err) { toast(err.message, "err"); }
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
  if (!domain) return toast("填写域名", "warn");
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
    toast("证书申请成功", "ok");
    showResult("证书成功", "id=" + r.id + "\n到期 " + new Date(r.expire_at * 1000).toLocaleString());
    await refreshCerts();
  } catch (e) {
    toast(e.message, "err");
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
  toast("已保存", "ok");
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
  toast("已保存", "ok");
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
