const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];
// 迁移旧版 XPanel localStorage 键
(function migrateLegacyKeys() {
  const pairs = [
    ["xpanel_token", "bpanel_token"],
    ["xpanel_theme_mode", "bpanel_theme_mode"],
    ["xpanel_theme", "bpanel_theme"],
  ];
  for (const [oldK, newK] of pairs) {
    if (!localStorage.getItem(newK) && localStorage.getItem(oldK)) {
      localStorage.setItem(newK, localStorage.getItem(oldK));
    }
  }
})();

const state = {
  token: localStorage.getItem("bpanel_token") || localStorage.getItem("xpanel_token") || "",
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
  dash: { title: "仪表盘", sub: "机器状态、节点数量与近两周流量" },
  servers: { title: "服务器", sub: "接入机器、安装 Agent、下发配置" },
  inbounds: { title: "入站节点", sub: "模板创建 · 侧滑编辑 · 二维码分享" },
  outbounds: { title: "出站与路由", sub: "WARP 出口与分流规则" },
  tunnels: { title: "中转隧道", sub: "端口转发与链式中转" },
  plans: { title: "套餐", sub: "流量额度与有效期" },
  users: { title: "成员", sub: "账号、套餐绑定与续期" },
  invites: { title: "邀请码", sub: "邀请注册并自动绑定套餐" },
  nodes: { title: "外部节点", sub: "导入其它面板的分享链接" },
  certs: { title: "证书", sub: "申请、上传并下发到 Agent" },
  nginx: { title: "Nginx", sub: "按服务器维护配置草稿" },
  logs: { title: "运行日志", sub: "节点 Xray / Agent 输出与操作审计" },
  traffic: { title: "流量统计", sub: "用量汇总与每日明细" },
  speed: { title: "连通测速", sub: "从主控探测 TCP / TLS 可达性" },
  links: { title: "分享链接", sub: "一键复制或扫码导入客户端" },
  sub: { title: "我的订阅", sub: "复制到客户端即可使用" },
  settings: { title: "设置", sub: "站点、外观与主控更新" },
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

function closeQR() {
  $("#qr-modal")?.classList.add("hidden");
  state._qrLink = "";
}

async function showQR(opts = {}) {
  const { title, name, link, inboundId } = opts;
  const modal = $("#qr-modal");
  if (!modal) return;
  $("#qr-title").textContent = title || "节点二维码";
  $("#qr-name").textContent = name || "";
  $("#qr-link").textContent = "生成中…";
  $("#qr-img").removeAttribute("src");
  modal.classList.remove("hidden");
  try {
    let data;
    if (inboundId) {
      try {
        data = await api("/api/inbounds/" + inboundId + "/qr");
      } catch (e) {
        if (!link) throw e;
        data = await api("/api/qr", { method: "POST", body: JSON.stringify({ text: link, size: 280 }) });
        data.link = link;
        data.name = name;
      }
    } else if (link) {
      data = await api("/api/qr", { method: "POST", body: JSON.stringify({ text: link, size: 280 }) });
      data.link = link;
      data.name = name;
    } else {
      throw new Error("没有可生成二维码的链接");
    }
    const url = data.data_url || (data.png_base64 ? "data:image/png;base64," + data.png_base64 : "");
    if (!url) throw new Error("二维码生成失败");
    $("#qr-img").src = url;
    $("#qr-link").textContent = data.link || link || "";
    $("#qr-name").textContent = data.name || name || data.protocol || "";
    state._qrLink = data.link || link || "";
    state._qrDataUrl = url;
  } catch (e) {
    closeQR();
    toast(e.message, "err");
  }
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

// —— 妙妙屋同款主题：light / dark / system 三段切换 ——
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

function syncThemeControls(modeKey) {
  document.querySelectorAll(".theme-seg").forEach((seg) => {
    seg.querySelectorAll("button[data-theme]").forEach((btn) => {
      const on = btn.dataset.theme === modeKey;
      btn.classList.toggle("active", on);
      btn.setAttribute("aria-pressed", on ? "true" : "false");
    });
  });
  const ico = $("#theme-ico");
  const lab = $("#theme-label");
  if (ico) ico.textContent = THEME_ICONS[modeKey] || "◐";
  if (lab) lab.textContent = THEME_LABELS[modeKey] || modeKey;
  const cycle = $("#theme-cycle");
  if (cycle) {
    const label = THEME_LABELS[modeKey] || modeKey;
    cycle.title = label;
    cycle.setAttribute("aria-label", label);
  }
  const sel = $("#set-theme");
  if (sel && sel.value !== modeKey) sel.value = modeKey;
}

function setTheme(mode, opts = {}) {
  const quiet = !!opts.quiet;
  let modeKey = mode || "system";
  // 兼容旧值
  if (modeKey === "auto" || modeKey === "anime" || modeKey === "flat" || String(modeKey).startsWith("auto-")) {
    modeKey = "system";
  }
  if (!THEME_MODES.includes(modeKey)) modeKey = "system";
  localStorage.setItem("bpanel_theme_mode", modeKey);

  const applied = resolveTheme(modeKey);
  document.documentElement.classList.toggle("dark", applied === "dark");
  document.documentElement.dataset.themeMode = modeKey;
  document.documentElement.removeAttribute("data-theme");

  const meta = document.querySelector("meta[name='theme-color']");
  if (meta) meta.setAttribute("content", applied === "dark" ? "#0c0e16" : "#f7f1ea");

  const label = THEME_LABELS[modeKey] || modeKey;
  const hint = $("#theme-hint");
  if (hint) {
    hint.textContent = modeKey === "system"
      ? `跟随系统 · 当前${applied === "dark" ? "深色" : "浅色"}`
      : label;
  }
  syncThemeControls(modeKey);
  if (!quiet) showThemeToast(label);
  return modeKey;
}

function cycleTheme() {
  const cur = localStorage.getItem("bpanel_theme_mode") || "system";
  let next = "light";
  if (cur === "light") next = "dark";
  else if (cur === "dark") next = "system";
  else next = "light";
  setTheme(next);
}

function bindThemeControls() {
  if (bindThemeControls._done) return;
  bindThemeControls._done = true;
  document.querySelectorAll(".theme-seg").forEach((seg) => {
    seg.addEventListener("click", (e) => {
      const btn = e.target.closest("button[data-theme]");
      if (!btn || !seg.contains(btn)) return;
      const mode = btn.dataset.theme;
      if (THEME_MODES.includes(mode)) setTheme(mode);
    });
  });
  const sel = $("#set-theme");
  if (sel) {
    sel.addEventListener("change", () => {
      const mode = sel.value || "system";
      if (THEME_MODES.includes(mode)) setTheme(mode);
    });
  }
  const cycle = $("#theme-cycle");
  if (cycle) cycle.onclick = () => cycleTheme();
}

function watchSystemTheme() {
  if (watchSystemTheme._mq) return;
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => {
    const mode = localStorage.getItem("bpanel_theme_mode") || "system";
    if (mode === "system") setTheme("system", { quiet: true });
  };
  if (mq.addEventListener) mq.addEventListener("change", onChange);
  else if (mq.addListener) mq.addListener(onChange);
  watchSystemTheme._mq = mq;
}

async function boot() {
  // 兼容旧 key
  let mode = localStorage.getItem("bpanel_theme_mode");
  if (!mode) {
    const legacy = localStorage.getItem("bpanel_theme");
    if (legacy === "light" || legacy === "dark") mode = legacy;
    else mode = "system";
    localStorage.setItem("bpanel_theme_mode", mode);
  }
  bindThemeControls();
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
  catch { state.token = ""; localStorage.removeItem("bpanel_token"); showAuth(); }
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
    localStorage.setItem("bpanel_token", state.token);
    await enterMain();
  } catch (e) {
    $("#auth-err").textContent = e.message;
  }
};

$$("#nav .nav").forEach((btn) => { btn.onclick = () => switchTab(btn.dataset.tab); });
bindThemeControls();
$("#btn-logout").onclick = () => { localStorage.removeItem("bpanel_token"); location.reload(); };
$("#modal-close") && ($("#modal-close").onclick = () => $("#modal").classList.add("hidden"));
$("#modal-close-2") && ($("#modal-close-2").onclick = () => $("#modal").classList.add("hidden"));
$("#modal-copy") && ($("#modal-copy").onclick = () => copyText($("#install-cmd")?.textContent || "").catch(() => {}));
$("#result-close") && ($("#result-close").onclick = closeResult);
$("#result-close-2") && ($("#result-close-2").onclick = closeResult);
$("#result-copy") && ($("#result-copy").onclick = () => copyText($("#result-body")?.textContent || "").catch(() => {}));
$("#qr-close") && ($("#qr-close").onclick = closeQR);
$("#qr-close-2") && ($("#qr-close-2").onclick = closeQR);
$("#qr-copy") && ($("#qr-copy").onclick = () => {
  copyText(state._qrLink || $("#qr-link")?.textContent || "").catch((e) => toast(e.message, "err"));
});
$("#qr-download") && ($("#qr-download").onclick = () => {
  const url = state._qrDataUrl || $("#qr-img")?.src;
  if (!url) return toast("没有可下载的二维码", "warn");
  const a = document.createElement("a");
  a.href = url;
  a.download = "bpanel-node-qr.png";
  a.click();
  toast("已开始下载", "ok", 1200);
});
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
    if (b.dataset.upd === "1") setTimeout(() => checkUpdate({ quiet: false }).catch(() => {}), 100);
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
    logs: refreshLogsPage,
    traffic: refreshTraffic,
    speed: () => Promise.resolve(),
    links: refreshLinks,
    settings: refreshSettings,
  };
  // stop logs auto-refresh when leaving page
  if (name !== "logs") stopLogsAutoRefresh();
  if (loaders[name]) loaders[name]().catch((e) => toast(e.message, "err"));
}

function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

function drawChart(series) {
  const c = $("#traffic-chart");
  if (!c) return;
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  const cssW = c.clientWidth || 1000;
  const cssH = 200;
  c.width = Math.floor(cssW * dpr);
  c.height = Math.floor(cssH * dpr);
  const ctx = c.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  const w = cssW;
  const h = cssH;
  ctx.clearRect(0, 0, w, h);

  const muted = cssVar("--muted-foreground", "#816055");
  const brand = cssVar("--brand-500", "#d97757");
  const upColor = "#d97757";
  const downColor = "#60a5fa";

  if (!series?.length) {
    ctx.fillStyle = muted;
    ctx.font = "14px system-ui, sans-serif";
    ctx.fillText("暂无流量数据 — 节点上报后将显示趋势", 16, h / 2);
    return;
  }

  const padL = 8, padR = 8, padT = 16, padB = 28;
  const max = Math.max(1, ...series.map((x) => Math.max(x.up || 0, x.down || 0, (x.up || 0) + (x.down || 0))));
  const n = series.length;
  const step = n <= 1 ? 0 : (w - padL - padR) / (n - 1);
  const yOf = (v) => padT + (1 - v / max) * (h - padT - padB);
  const xOf = (i) => padL + i * step;

  // grid
  ctx.strokeStyle = "rgba(137,110,96,0.18)";
  ctx.lineWidth = 1;
  for (let g = 0; g <= 3; g++) {
    const y = padT + ((h - padT - padB) * g) / 3;
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(w - padR, y);
    ctx.stroke();
  }

  const drawLine = (key, color, dashed) => {
    ctx.strokeStyle = color;
    ctx.lineWidth = 2;
    ctx.setLineDash(dashed ? [5, 4] : []);
    ctx.beginPath();
    series.forEach((p, i) => {
      const val = key === "total" ? (p.up || 0) + (p.down || 0) : (p[key] || 0);
      const x = xOf(i);
      const y = yOf(val);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();
    ctx.setLineDash([]);
  };

  // area for total
  ctx.beginPath();
  series.forEach((p, i) => {
    const x = xOf(i);
    const y = yOf((p.up || 0) + (p.down || 0));
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  });
  ctx.lineTo(xOf(n - 1), h - padB);
  ctx.lineTo(xOf(0), h - padB);
  ctx.closePath();
  ctx.fillStyle = "rgba(217,119,87,0.10)";
  ctx.fill();

  drawLine("up", upColor, false);
  drawLine("down", downColor, false);
  drawLine("total", brand, true);

  // points + day labels
  ctx.fillStyle = muted;
  ctx.font = "11px system-ui, sans-serif";
  series.forEach((p, i) => {
    const x = xOf(i);
    const showLabel = n <= 8 || i === 0 || i === n - 1 || i % Math.ceil(n / 6) === 0;
    if (showLabel) {
      const label = String(p.day || "").slice(5); // MM-DD
      const tw = ctx.measureText(label).width;
      ctx.fillText(label, x - tw / 2, h - 8);
    }
    // dots
    ctx.beginPath();
    ctx.fillStyle = upColor;
    ctx.arc(x, yOf(p.up || 0), 2.2, 0, Math.PI * 2);
    ctx.fill();
    ctx.beginPath();
    ctx.fillStyle = downColor;
    ctx.arc(x, yOf(p.down || 0), 2.2, 0, Math.PI * 2);
    ctx.fill();
  });
}

function dashStatCard({ icon, label, value, hint, tone }) {
  return `<div class="stat stat-rich${tone ? " tone-" + tone : ""}">
    <div class="stat-top">
      <span class="stat-ico" aria-hidden="true">${icon}</span>
    </div>
    <div class="n">${value}</div>
    <div class="l">${escapeHtml(label)}</div>
    ${hint ? `<div class="stat-hint">${escapeHtml(hint)}</div>` : ""}
  </div>`;
}

function renderDashServers(list) {
  const box = $("#dash-servers");
  if (!box) return;
  if (!list?.length) {
    box.innerHTML = `<div class="dash-empty">暂无服务器 · 点击上方「接入服务器」开始</div>`;
    return;
  }
  box.innerHTML = list.map((s) => {
    const st = s.status || "offline";
    const label = st === "online" ? "在线" : st === "pending" ? "待装" : "离线";
    const chip = st === "online" ? "on" : st === "pending" ? "pending" : "off";
    const host = s.domain || s.public_ip || s.hostname || "—";
    const xray = s.xray_running ? "Xray 运行" : "Xray 停";
    return `<div class="dash-srv" data-go="servers" title="${escapeHtml(s.name)}">
      <span class="dot ${chip}"></span>
      <div class="ds-main">
        <div class="ds-name">${escapeHtml(s.name)} <span class="chip ${chip}" style="margin-left:.25rem">${label}</span></div>
        <div class="ds-meta">${escapeHtml(host)} · ${xray}</div>
      </div>
      <div class="ds-speed">↑ ${fmtSpeed(s.speed_up)}<br>↓ ${fmtSpeed(s.speed_down)}</div>
    </div>`;
  }).join("");
  box.querySelectorAll("[data-go]").forEach((el) => {
    el.onclick = () => switchTab(el.dataset.go);
  });
}

function renderDashProtocols(list) {
  const box = $("#dash-protocols");
  if (!box) return;
  if (!list?.length) {
    box.innerHTML = `<div class="dash-empty">暂无入站节点</div>`;
    return;
  }
  const max = Math.max(1, ...list.map((p) => p.count || 0));
  box.innerHTML = list.map((p) => {
    const pct = Math.round(((p.count || 0) / max) * 100);
    return `<div class="proto-row">
      <span class="p-name" title="${escapeHtml(p.protocol)}">${escapeHtml(p.protocol)}</span>
      <span class="p-bar"><i style="width:${pct}%"></i></span>
      <span class="p-cnt">${p.count || 0}</span>
    </div>`;
  }).join("");
}

function renderDashAlerts(list) {
  const box = $("#dash-alerts");
  if (!box) return;
  box.innerHTML = (list || []).map((a) => {
    const level = a.level || "info";
    const go = a.go ? ` data-go="${escapeHtml(a.go)}"` : "";
    return `<div class="alert-item ${escapeHtml(level)}"${go}>
      <span class="a-dot"></span>
      <span>${escapeHtml(a.text || "")}</span>
    </div>`;
  }).join("");
  box.querySelectorAll("[data-go]").forEach((el) => {
    el.onclick = () => {
      if (el.dataset.go) switchTab(el.dataset.go);
    };
  });
}

async function refreshDash() {
  const d = await api("/api/dashboard");
  if ($("#dash-ver")) $("#dash-ver").textContent = "v" + (d.version || state.meta?.version || "?");

  const en = d.inbounds_enabled != null ? d.inbounds_enabled : d.inbounds;
  const offline = d.offline || 0;
  const pending = d.pending || 0;
  const cards = [
    { icon: "☺", label: "成员", value: d.users ?? 0, hint: "注册账号", tone: "info" },
    { icon: "⬡", label: "服务器", value: d.servers ?? 0, hint: `在线 ${d.online || 0} · 待装 ${pending}`, tone: "info" },
    { icon: "●", label: "在线", value: d.online ?? 0, hint: offline ? `离线 ${offline}` : "全部在线", tone: "ok" },
    { icon: "○", label: "离线", value: offline, hint: pending ? `另有 ${pending} 待装` : "无响应", tone: offline ? "warn" : "" },
    { icon: "◎", label: "入站节点", value: d.inbounds ?? 0, hint: `启用 ${en}`, tone: "info" },
    { icon: "⚡", label: "Xray 运行", value: d.xray_running ?? 0, hint: `共 ${d.servers || 0} 台`, tone: "ok" },
    { icon: "▤", label: "套餐", value: d.plans ?? 0, hint: "流量套餐", tone: "" },
    { icon: "⚿", label: "证书", value: d.certs ?? 0, hint: (d.certs_expiring || 0) ? `${d.certs_expiring} 即将到期` : "有效证书", tone: (d.certs_expiring || 0) ? "warn" : "" },
    { icon: "↑", label: "今日上行", value: fmtBytes(d.today_up), hint: "当日累计", tone: "" },
    { icon: "↓", label: "今日下行", value: fmtBytes(d.today_down), hint: "当日累计", tone: "" },
    { icon: "↗", label: "实时上行", value: fmtSpeed(d.speed_up), hint: "全机合计", tone: "ok" },
    { icon: "↘", label: "实时下行", value: fmtSpeed(d.speed_down), hint: "全机合计", tone: "ok" },
    { icon: "⬆", label: "累计上行", value: fmtBytes(d.traffic_up), hint: "历史总计", tone: "" },
    { icon: "⬇", label: "累计下行", value: fmtBytes(d.traffic_down), hint: "历史总计", tone: "" },
  ];
  if ($("#dash-stats")) {
    $("#dash-stats").innerHTML = cards.map(dashStatCard).join("");
  }

  const series = d.series || [];
  state._lastDashSeries = series;
  drawChart(series);
  let sumUp = 0, sumDown = 0;
  series.forEach((p) => { sumUp += p.up || 0; sumDown += p.down || 0; });
  if ($("#dash-chart-summary")) {
    $("#dash-chart-summary").innerHTML = series.length
      ? `<span>14 日上行 <strong>${fmtBytes(sumUp)}</strong></span>
         <span>14 日下行 <strong>${fmtBytes(sumDown)}</strong></span>
         <span>合计 <strong>${fmtBytes(sumUp + sumDown)}</strong></span>
         <span>今日 <strong>↑ ${fmtBytes(d.today_up)} / ↓ ${fmtBytes(d.today_down)}</strong></span>`
      : `<span>等待流量数据上报…</span>`;
  }

  renderDashServers(d.servers_preview || []);
  renderDashProtocols(d.protocols || []);
  renderDashAlerts(d.alerts || []);
}
if ($("#btn-refresh-dash")) $("#btn-refresh-dash").onclick = () => refreshDash().catch((e) => toast(e.message, "err"));
$("#dash-go-srv") && ($("#dash-go-srv").onclick = () => switchTab("servers"));
// resize chart when window changes
window.addEventListener("resize", () => {
  if (state.currentTab === "dash" && state._lastDashSeries) drawChart(state._lastDashSeries);
});

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
  sel.innerHTML = '<option value="0">不使用证书（Reality / 无加密）</option>' +
    state.certs.filter((c) => c.status === "active").map((c) =>
      `<option value="${c.id}">${escapeHtml(c.domain)}</option>`).join("");
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
  // 在线优先，其次待安装，最后离线；同组按名称
  let list = [...state.servers].sort((a, b) => {
    const rank = (s) => (s.online ? 0 : s.status === "pending" ? 1 : 2);
    const d = rank(a) - rank(b);
    if (d !== 0) return d;
    return String(a.name || "").localeCompare(String(b.name || ""), "zh");
  });
  if (state.srvFilter === "online") list = list.filter((s) => s.online);
  if (state.srvFilter === "offline") list = list.filter((s) => !s.online && s.status !== "pending");
  const box = $("#server-list");
  box.innerHTML = list.map((s) => {
    const chip = s.online ? "on" : s.status === "pending" ? "pending" : "off";
    const label = s.online ? "在线" : s.status === "pending" ? "待安装" : "离线";
    const xray = s.xray_running ? "运行中" : "未运行";
    const xver = s.xray_version ? (" v" + String(s.xray_version).replace(/^v/, "")) : "";
    return `<div class="server-card">
      <div class="title">
        <strong><span class="dot ${chip}"></span>${escapeHtml(s.name)}</strong>
        <span class="chip ${chip}">${label}</span>
      </div>
      <div class="meta">${escapeHtml(s.hostname || "主机名未知")} · ${escapeHtml(s.domain || s.public_ip || "未上报 IP")}</div>
      <div class="meta badge-speed">↑ ${fmtBytes(s.traffic_up)} · ↓ ${fmtBytes(s.traffic_down)} · 实时 ${fmtSpeed(s.speed_up)} / ${fmtSpeed(s.speed_down)}</div>
      <div class="meta">Xray ${xray}${escapeHtml(xver)} · 配置 v${s.config_version}${s.agent_version ? " · Agent v" + escapeHtml(s.agent_version) : ""}${s.tags ? " · " + escapeHtml(s.tags) : ""}</div>
      ${s.agent_error ? `<div class="meta err-line">异常：${escapeHtml(s.agent_error).slice(0, 200)}</div>` : ""}
      <div class="row" style="margin:0">
        <button class="small" data-act="logs" data-id="${s.id}" data-name="${escapeHtml(s.name)}">日志 / 版本</button>
        <button class="small" data-act="install" data-id="${s.id}">安装命令</button>
        <button class="small primary" data-act="bump" data-id="${s.id}">下发配置</button>
        <button class="small danger" data-act="del" data-id="${s.id}">删除</button>
      </div>
    </div>`;
  }).join("") || `<div class="empty-state"><h3>尚未接入服务器</h3><p>添加一台机器并安装 Agent 后，即可下发节点配置</p></div>`;
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!(await uiConfirm("删除后该机器上的 Agent 将无法再连接，确定继续？", "删除服务器"))) return;
      await api("/api/servers/" + id, { method: "DELETE" });
      toast("服务器已删除", "ok");
      await refreshServers();
    }
    if (btn.dataset.act === "bump") {
      await api("/api/servers/" + id + "/bump-config", { method: "POST", body: "{}" });
      toast("已通知 Agent 拉取最新配置", "ok");
      await refreshServers();
    }
    if (btn.dataset.act === "logs") {
      openServerLogs(id, btn.dataset.name || "");
    }
    if (btn.dataset.act === "install") {
      const info = await api("/api/servers/" + id + "/install-cmd");
      $("#modal-title").textContent = "安装 Agent · " + (info.name || "");
      $("#install-cmd").textContent =
        "# Linux 一键安装（含 Xray latest）\n" + (info.one_click_cmd || info.install_cmd || "") +
        "\n\n# 指定 Xray 版本示例\n" +
        `curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install-agent.sh | sudo bash -s -- -m ${info.master_url} -t ${info.token} --with-xray --xray-version v26.3.27` +
        "\n\n# Docker\n" + info.docker_cmd +
        "\n\n# 二进制\n" + info.binary_cmd;
      $("#modal").classList.remove("hidden");
    }
  };
}

// —— 服务器日志 / Xray 版本 ——
state._logServerId = "";
state._xrayVersions = null;
state._logsPageServerId = "";
state._logsAutoTimer = null;
state._logsOverview = null;

function fmtUnix(ts) {
  if (!ts) return "—";
  const d = new Date(Number(ts) * 1000);
  if (Number.isNaN(d.getTime())) return "—";
  const p = (n) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function stopLogsAutoRefresh() {
  if (state._logsAutoTimer) {
    clearInterval(state._logsAutoTimer);
    state._logsAutoTimer = null;
  }
}

function startLogsAutoRefresh() {
  stopLogsAutoRefresh();
  if (!$("#logs-auto")?.checked) return;
  state._logsAutoTimer = setInterval(() => {
    if (state.currentTab !== "logs") return;
    refreshLogsPage({ quiet: true }).catch(() => {});
  }, 8000);
}

async function refreshLogsPage(opts = {}) {
  const quiet = !!opts.quiet;
  try {
    const data = await api("/api/logs");
    state._logsOverview = data;
    renderLogsServerList(data.servers || []);
    renderAuditList(data.audit || []);
    const sid = state._logsPageServerId || (data.servers?.[0]?.id || "");
    if (sid) {
      state._logsPageServerId = sid;
      await loadLogsPageBody(sid, { quiet });
    } else if ($("#logs-view-body")) {
      $("#logs-view-body").textContent = "暂无服务器。请先在「服务器」页接入 Agent。";
    }
    startLogsAutoRefresh();
  } catch (e) {
    if (!quiet) toast(e.message, "err");
    throw e;
  }
}

function renderLogsServerList(list) {
  const box = $("#logs-server-list");
  if (!box) return;
  if (!list.length) {
    box.innerHTML = `<div class="dash-empty">暂无服务器</div>`;
    return;
  }
  box.innerHTML = list.map((s) => {
    const on = s.online ? "on" : (s.status === "pending" ? "pending" : "off");
    const label = s.online ? "在线" : (s.status === "pending" ? "待装" : "离线");
    const active = s.id === state._logsPageServerId ? " active" : "";
    const xray = s.xray_running ? "Xray 运行" : "Xray 停";
    const n = s.log_lines || 0;
    return `<button type="button" class="logs-srv-item${active}" data-id="${s.id}">
      <span class="ls-name"><span class="dot ${on}"></span>${escapeHtml(s.name)} <span class="chip ${on}">${label}</span></span>
      <span class="ls-meta">${xray}${s.xray_version ? " · v" + escapeHtml(String(s.xray_version).replace(/^v/, "")) : ""} · 缓存 ${n} 行</span>
      ${s.agent_error ? `<span class="ls-meta err-line">${escapeHtml(String(s.agent_error).slice(0, 80))}</span>` : ""}
    </button>`;
  }).join("");
  box.onclick = (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    state._logsPageServerId = btn.dataset.id;
    renderLogsServerList(state._logsOverview?.servers || list);
    loadLogsPageBody(btn.dataset.id).catch((err) => toast(err.message, "err"));
  };
}

function renderAuditList(list) {
  const box = $("#logs-audit-list");
  if (!box) return;
  if (!list.length) {
    box.innerHTML = `<div class="dash-empty">暂无审计记录</div>`;
    return;
  }
  box.innerHTML = list.map((a) => `
    <div class="item">
      <div class="item-main">
        <div class="item-title">
          <strong>${escapeHtml(a.action || "")}</strong>
          <span class="chip">${escapeHtml(a.actor || "system")}</span>
        </div>
        <div class="meta">${escapeHtml(a.detail || "")}</div>
      </div>
      <span class="audit-time">${fmtUnix(a.created_at)}</span>
    </div>`).join("");
}

async function loadLogsPageBody(id, opts = {}) {
  const quiet = !!opts.quiet;
  const body = $("#logs-view-body");
  if (!quiet && body) body.textContent = "加载中…";
  try {
    const d = await api("/api/servers/" + id + "/logs?lines=400");
    const lines = d.lines || [];
    if (body) {
      body.textContent = lines.length ? lines.join("\n") : "（空）";
      if (!quiet) body.scrollTop = body.scrollHeight;
    }
    const srv = (state._logsOverview?.servers || []).find((s) => s.id === id);
    if ($("#logs-view-title")) $("#logs-view-title").textContent = (d.name || srv?.name || "节点") + " · 日志";
    if ($("#logs-view-sub")) {
      const online = d.online ? "在线" : "离线";
      const run = d.xray_running ? "Xray 运行中" : "Xray 未运行";
      const ver = d.xray_version ? ("v" + String(d.xray_version).replace(/^v/, "")) : "版本未知";
      const av = d.agent_version ? ("Agent v" + d.agent_version) : "Agent ?";
      $("#logs-view-sub").textContent = `${online} · ${run} · ${ver} · ${av} · ${d.count || lines.length} 行缓存`;
    }
    return d;
  } catch (e) {
    if (body) body.textContent = "加载失败: " + e.message;
    throw e;
  }
}

$("#btn-logs-refresh") && ($("#btn-logs-refresh").onclick = () => {
  refreshLogsPage().catch((e) => toast(e.message, "err"));
});
$("#logs-auto") && ($("#logs-auto").onchange = () => {
  if ($("#logs-auto").checked) startLogsAutoRefresh();
  else stopLogsAutoRefresh();
});
$("#btn-logs-copy") && ($("#btn-logs-copy").onclick = () => {
  copyText($("#logs-view-body")?.textContent || "").then(() => toast("已复制", "ok", 1200)).catch((e) => toast(e.message, "err"));
});
$("#btn-logs-open-modal") && ($("#btn-logs-open-modal").onclick = () => {
  const id = state._logsPageServerId;
  if (!id) return toast("请先选择服务器", "warn");
  const srv = (state._logsOverview?.servers || []).find((s) => s.id === id);
  openServerLogs(id, srv?.name || "");
});

async function loadXrayVersionOptions(current) {
  const sel = $("#log-xray-ver");
  if (!sel) return;
  try {
    if (!state._xrayVersions) {
      state._xrayVersions = await api("/api/xray/versions");
    }
    const d = state._xrayVersions;
    const latest = d.latest || "latest";
    const rels = d.releases || [];
    let html = `<option value="latest">latest（${escapeHtml(latest)}）</option>`;
    rels.forEach((r) => {
      const tag = r.tag || "";
      if (!tag) return;
      const pre = r.prerelease ? " · pre" : "";
      html += `<option value="${escapeHtml(tag)}">${escapeHtml(tag)}${pre}</option>`;
    });
    sel.innerHTML = html;
    if (current) {
      const cur = String(current).startsWith("v") ? current : "v" + current;
      const opt = [...sel.options].find((o) => o.value === cur || o.value === current);
      if (opt) sel.value = opt.value;
      else if (latest) sel.value = "latest";
    }
  } catch (e) {
    sel.innerHTML = `<option value="latest">latest</option>`;
    toast("拉取 Xray 版本列表失败: " + e.message, "warn");
  }
}

async function refreshLogBody() {
  const id = state._logServerId;
  if (!id) return;
  const d = await api("/api/servers/" + id + "/logs?lines=300");
  const lines = d.lines || [];
  const body = $("#log-body");
  if (body) {
    body.textContent = lines.length ? lines.join("\n") : "（暂无日志 — Agent 上线并运行 Xray 后将自动上报）";
    body.scrollTop = body.scrollHeight;
  }
  if ($("#log-sub")) {
    const run = d.xray_running ? "运行中" : "未运行";
    const ver = d.xray_version ? ("v" + String(d.xray_version).replace(/^v/, "")) : "未知";
    $("#log-sub").textContent = `Xray ${run} · 版本 ${ver} · ${lines.length} 行`;
  }
  return d;
}

async function openServerLogs(id, name) {
  state._logServerId = id;
  if ($("#log-title")) $("#log-title").textContent = "日志 · " + (name || id.slice(0, 8));
  if ($("#log-body")) $("#log-body").textContent = "加载中…";
  $("#log-modal")?.classList.remove("hidden");
  try {
    const d = await refreshLogBody();
    await loadXrayVersionOptions(d?.xray_version || "");
  } catch (e) {
    if ($("#log-body")) $("#log-body").textContent = "加载失败: " + e.message;
    toast(e.message, "err");
  }
}

function closeLogModal() {
  $("#log-modal")?.classList.add("hidden");
  state._logServerId = "";
}

$("#log-close") && ($("#log-close").onclick = closeLogModal);
$("#log-modal") && ($("#log-modal").onclick = (e) => {
  if (e.target === $("#log-modal")) closeLogModal();
});
$("#log-refresh") && ($("#log-refresh").onclick = () => {
  refreshLogBody().catch((e) => toast(e.message, "err"));
});
$("#log-copy") && ($("#log-copy").onclick = () => {
  copyText($("#log-body")?.textContent || "").then(() => toast("已复制", "ok", 1200)).catch((e) => toast(e.message, "err"));
});
$("#log-xray-restart") && ($("#log-xray-restart").onclick = async () => {
  const id = state._logServerId;
  if (!id) return;
  try {
    const d = await api("/api/servers/" + id + "/xray/restart", { method: "POST", body: "{}" });
    toast(d.message || "已通知重启", "ok");
    setTimeout(() => refreshLogBody().catch(() => {}), 3000);
  } catch (e) { toast(e.message, "err"); }
});
$("#log-xray-install") && ($("#log-xray-install").onclick = async () => {
  const id = state._logServerId;
  if (!id) return;
  const custom = ($("#log-xray-custom")?.value || "").trim();
  const ver = custom || ($("#log-xray-ver")?.value || "latest");
  if (!(await uiConfirm(`在此服务器上安装/切换 Xray 到 ${ver}？\n将下载官方二进制并重启进程。`, "安装 Xray"))) return;
  try {
    const d = await api("/api/servers/" + id + "/xray/install", {
      method: "POST",
      body: JSON.stringify({ version: ver }),
    });
    toast(d.message || "已下发安装任务", "ok");
    if ($("#log-xray-custom")) $("#log-xray-custom").value = "";
    setTimeout(() => {
      refreshLogBody().catch(() => {});
      refreshServers().catch(() => {});
    }, 5000);
  } catch (e) { toast(e.message, "err"); }
});
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
  "anytls-tls": {
    // 需支持 anytls 的内核；官方 Xray 主线可能不支持，下发时会被跳过以免拖垮全部节点
    proto: "anytls", port: 443, network: "tcp", security: "tls",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h2,http/1.1", method: "aes-256-gcm",
  },
  "hysteria2-tls": {
    // 需要绑定证书（ACME），否则不会下发
    proto: "hysteria", port: 443, network: "hysteria", security: "tls",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "h3", method: "aes-256-gcm",
  },
  "socks-auth": {
    proto: "socks", port: 1080, network: "tcp", security: "none",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
  "http-auth": {
    proto: "http", port: 8081, network: "tcp", security: "none",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
  "mixed-auth": {
    proto: "mixed", port: 1080, network: "tcp", security: "none",
    flow: "", path: "", host: "", sni: "", dest: "",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
  "dokodemo": {
    proto: "dokodemo-door", port: 10080, network: "tcp", security: "none",
    flow: "", path: "80", host: "127.0.0.1", sni: "", dest: "",
    fp: "chrome", alpn: "", method: "aes-256-gcm",
  },
  "wireguard": {
    proto: "wireguard", port: 51820, network: "tcp", security: "none",
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
  const meta = st.bpanelMeta || st.xpanelMeta || {};
  return { uuid, flow, password, method, pbk: meta.publicKey || "" };
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
  $("#in-editor-hint").textContent = "选择模板后按需微调，展开分区可编辑更多选项。";
  $$("#in-editor details.fold").forEach((d, i) => {
    d.open = i < 3;
  });
}

function fillInboundForm(inb) {
  $("#in-id").value = inb.id || "";
  $("#in-editor-title").textContent = inb.id ? `编辑节点 · ${inb.share_name || inb.tag || "#" + inb.id}` : "新建节点";
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
  $("#in-editor-hint").textContent = "正在编辑已有节点。密钥默认折叠；需要完整自定义时请使用高级 JSON。";
  $$("#in-editor details.fold").forEach((d, i) => {
    d.open = i < 3;
  });
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

  const protocol = $("#in-proto").value;
  const payload = {
    server_id: $("#in-server").value,
    protocol,
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
    enabled: $("#in-enabled").value !== "0",
  };
  // Only send identity fields when user explicitly set them (avoid server treating
  // empty uuid as "regenerate" or always-present method as client rebuild).
  if (uuidTpl) payload.uuid = uuidTpl;
  if (flow) payload.flow = flow;
  if (passwordTpl) payload.password = passwordTpl;
  if (protocol === "shadowsocks" || protocol === "ss") {
    payload.method = $("#in-method").value;
  }
  // socks/http 用 uuid 字段当用户名（可空）
  if (["socks", "http", "mixed", "anytls", "hysteria", "hysteria2"].includes(protocol)) {
    if (!payload.password && $("#in-password")?.value) payload.password = $("#in-password").value.trim();
  }

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
  const all = [...(state.inboundsCache || [])].sort((a, b) => (b.id || 0) - (a.id || 0));
  const list = all.filter((inb) => {
    let stream = {};
    try { stream = JSON.parse(inb.stream_json || "{}"); } catch { /* */ }
    return matchInboundFilter(inb, stream);
  });
  if ($("#in-count")) $("#in-count").textContent = list.length === all.length
    ? `共 ${list.length} 个`
    : `显示 ${list.length} / ${all.length}`;
  const box = $("#inbound-list");
  if (!box) return;
  if (!list.length) {
    box.innerHTML = `<div class="empty-state"><h3>${all.length ? "没有符合条件的节点" : "还没有入站节点"}</h3>
      <p>${all.length ? "试试清空搜索或筛选条件" : "使用上方模板，或点击「新建节点」开始"}</p>
      ${!all.length ? '<button type="button" class="primary" id="btn-empty-new">新建节点</button>' : ""}</div>`;
    const bn = $("#btn-empty-new");
    if (bn) bn.onclick = () => { resetInboundForm(); openNodeDrawer(); };
    return;
  }
  const secLabel = (s) => ({ none: "无加密", tls: "TLS", reality: "Reality" }[s] || s);
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
          <span class="chip">${escapeHtml(net)} · ${escapeHtml(secLabel(sec))}</span>
          <span class="chip">端口 ${inb.port}</span>
          ${en ? '<span class="chip on">已启用</span>' : '<span class="chip off">已禁用</span>'}
        </div>
        <div class="meta">标识 ${escapeHtml(inb.tag)} · 倍率 ×${inb.multiplier || 1}${inb.remark ? " · " + escapeHtml(inb.remark) : ""}</div>
      </div>
      <div class="item-actions row">
        <button class="small" data-act="edit" data-id="${inb.id}">编辑</button>
        <button class="small" data-act="qr" data-id="${inb.id}" ${link ? "" : "disabled"} title="二维码">二维码</button>
        <button class="small" data-act="copy" data-id="${inb.id}" ${link ? "" : "disabled"}>复制</button>
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
    if (btn.dataset.act === "qr") {
      const inb = (state.inboundsCache || []).find((x) => String(x.id) === String(id));
      showQR({
        inboundId: id,
        name: inb ? (inb.share_name || inb.tag) : "",
        title: "节点二维码",
      });
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
    else if ($("#qr-modal") && !$("#qr-modal").classList.contains("hidden")) closeQR();
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
    settings.clients = [{ id: p.uuid || "(auto)", email: "default@bpanel", flow: p.flow || "" }];
    if (p.protocol !== "vless") delete settings.decryption;
  } else if (p.protocol === "trojan") {
    settings.clients = [{ password: p.password || "(auto)", email: "trojan@bpanel" }];
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
    // 拦截会导致整机 Xray 挂掉的配置
    if (payload.security === "tls" && !(Number(payload.cert_id) > 0) &&
        ["vless", "vmess", "trojan", "hysteria", "anytls"].includes(payload.protocol)) {
      return toast("TLS 需要先绑定有效证书（证书 ACME），或改用 Reality / none", "warn");
    }
    if (payload.protocol === "anytls") {
      toast("提示：AnyTLS 需支持该协议的 Xray；官方内核下发时会跳过，以免影响其它节点", "info", 4000);
    }
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
    const pk = r.settings?.bpanelMeta?.publicKey || r.settings?.xpanelMeta?.publicKey;
    if (pk) msg += "\n\nReality publicKey:\n" + pk;
    toast(id ? "节点已更新并下发" : "节点已创建并下发", "ok");
    showResult(id ? "更新成功" : "创建成功", msg);
    const newId = r.id || id;
    const share = r.share_link || "";
    resetInboundForm();
    closeNodeDrawer();
    await refreshInbounds();
    // 新建/更新后若有分享链接，弹出二维码方便扫码导入
    if (share || newId) {
      setTimeout(() => {
        showQR({
          title: id ? "节点二维码" : "创建成功 · 扫码导入",
          name: r.tag || "",
          link: share,
          inboundId: newId,
        });
      }, 200);
    }
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
  const srv = (state.servers || []).find((s) => s.id === server_id);
  if (srv && !srv.online && srv.status !== "online") {
    if (!(await uiConfirm("所选服务器当前离线，节点会创建但需 Agent 上线后才会生效。继续？", "服务器离线"))) return;
  }
  const port = Number(getTplValue("#in-port-tpl", "#in-port")) || 443;
  let dest = (getTplValue("#in-dest-tpl", "#in-dest") || "").trim();
  let sni = (getTplValue("#in-sni-tpl", "#in-sni") || "").trim();
  if (!sni) sni = "www.microsoft.com";
  if (!dest) dest = sni.includes(":") ? sni : (sni + ":443");
  else if (!dest.includes(":")) dest = dest + ":443";
  try {
    const r = await api("/api/inbounds/quick-reality", {
      method: "POST",
      body: JSON.stringify({
        server_id,
        port,
        dest,
        sni,
        flow: getSelectOrCustomSimple("#in-flow", "#in-flow-custom") || "xtls-rprx-vision",
        name: $("#in-tag")?.value.trim() || undefined,
      }),
    });
    toast("Reality 节点已创建并下发", "ok");
    const tip = [
      `服务器: ${r.server || ""}`,
      `地址: ${r.address || ""}:${port}`,
      `SNI: ${r.sni || sni}`,
      `Dest: ${r.dest || dest}`,
      `PublicKey (pbk):\n${r.public_key || ""}`,
      `ShortId: ${r.short_id || ""}`,
      "",
      r.note || "",
      "",
      "分享链接:",
      r.share_link || "",
    ].join("\n");
    showResult("Reality 已创建", tip);
    await refreshInbounds();
    if (r.share_link || r.id) {
      setTimeout(() => showQR({
        title: "Reality · 扫码导入",
        name: r.tag || "reality",
        link: r.share_link || "",
        inboundId: r.id,
      }), 200);
    }
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
    </div></div>`).join("") || '<div class="empty-state"><h3>暂无出站</h3><p>可添加 freedom / WARP 等出口</p></div>';
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
    <button class="small danger" data-id="${r.id}">删除</button></div>`).join("") || '<div class="empty-state"><h3>暂无路由规则</h3><p>按域名或 IP 分流到指定出口</p></div>';
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
  const links = data.links || [];
  state._linkByIdx = links.map((l) => l.link || "");
  state._linkMeta = links;
  $("#links-list").innerHTML = links.map((l, i) => `
    <div class="item" style="align-items:flex-start"><div style="flex:1">
      <strong>${escapeHtml(l.name)}</strong> · ${escapeHtml(l.protocol)} ${escapeHtml(l.address)}:${l.port}
      <div class="mono" style="margin-top:.4rem">${escapeHtml(l.link || "(empty)")}</div>
    </div>
    <div class="item-actions row">
      <button class="small" data-qr-idx="${i}" ${l.link ? "" : "disabled"}>二维码</button>
      <button class="small" data-copy-idx="${i}">复制</button>
    </div></div>`).join("") || '<div class="empty-state"><h3>暂无分享链接</h3><p>创建并启用入站节点后，这里会自动列出</p></div>';
  $("#links-list").onclick = async (e) => {
    const qbtn = e.target.closest("button[data-qr-idx]");
    if (qbtn) {
      const i = Number(qbtn.dataset.qrIdx);
      const meta = (state._linkMeta || [])[i] || {};
      const link = (state._linkByIdx || [])[i] || "";
      showQR({ title: "节点二维码", name: meta.name || "", link, inboundId: meta.id });
      return;
    }
    const btn = e.target.closest("button[data-copy-idx]");
    if (!btn) return;
    const link = (state._linkByIdx || [])[Number(btn.dataset.copyIdx)] || "";
    try {
      await copyText(link);
      btn.textContent = "已复制";
      setTimeout(() => (btn.textContent = "复制"), 1200);
    } catch (err) {
      toast(err.message || "复制失败", "err");
    }
  };
}
$("#btn-refresh-links").onclick = () => refreshLinks();

async function refreshSettings() {
  const data = await api("/api/settings");
  const s = data.settings || {};
  $("#set-site").value = s.site_name || "BPanel";
  let themeMode = localStorage.getItem("bpanel_theme_mode") || s.theme || "system";
  if (!THEME_MODES.includes(themeMode)) themeMode = "system";
  setTheme(themeMode, { quiet: true });
  $("#set-probe").value = s.probe_mode || "off";
  $("#set-acme-email").value = s.acme_email || "";
  $("#set-cf-token").value = s.cf_dns_api_token || "";
  $("#set-dns-key").value = s.dns_api_key || "";
  $("#set-dns-secret").value = s.dns_api_secret || "";
  $("#set-webhook").value = s.webhook_url || "";
  if (s.acme_email && $("#acme-email") && !$("#acme-email").value) $("#acme-email").value = s.acme_email;
  // 自动检查更新（不阻塞设置页）
  checkUpdate({ quiet: true }).catch(() => {});
}

async function checkUpdate(opts = {}) {
  const quiet = !!opts.quiet;
  const curEl = $("#upd-current");
  const latEl = $("#upd-latest");
  const badge = $("#upd-badge");
  const msg = $("#upd-msg");
  const notes = $("#upd-notes");
  const btnRun = $("#btn-upd-run");
  if (curEl && state.meta?.version) curEl.textContent = "v" + state.meta.version;
  try {
    if (!quiet && msg) msg.textContent = "正在检查 GitHub Release…";
    const d = await api("/api/system/update/check");
    if (curEl) curEl.textContent = "v" + (d.current || state.meta?.version || "?");
    if (latEl) latEl.textContent = d.latest ? ("v" + d.latest) : "未知";
    if (badge) {
      badge.classList.remove("hidden");
      if (d.update_available) {
        badge.textContent = "有新版本";
        badge.className = "chip on";
      } else if (d.latest) {
        badge.textContent = "已是最新";
        badge.className = "chip";
      } else {
        badge.textContent = "检查失败";
        badge.className = "chip off";
      }
    }
    if (btnRun) btnRun.disabled = !d.update_available || !d.download_url;
    if (msg) {
      if (d.update_available) {
        msg.textContent = `发现新版本 v${d.latest}，资源 ${d.asset || ""}。点击「一键更新」下载并重启主控。`;
      } else if (d.error) {
        msg.textContent = "检查失败: " + d.error;
      } else {
        msg.textContent = `当前已是最新（v${d.current}）。仓库: ${d.repo || ""}`;
      }
    }
    if (notes) {
      if (d.notes) {
        notes.classList.remove("hidden");
        notes.textContent = d.notes;
      } else {
        notes.classList.add("hidden");
        notes.textContent = "";
      }
    }
    state._updateInfo = d;
    if (!quiet) toast(d.update_available ? "发现新版本 v" + d.latest : "已是最新版本", d.update_available ? "info" : "ok");
    return d;
  } catch (e) {
    if (msg) msg.textContent = "检查失败: " + e.message;
    if (btnRun) btnRun.disabled = true;
    if (!quiet) toast(e.message, "err");
    throw e;
  }
}

async function runSelfUpdate() {
  const info = state._updateInfo;
  if (!info?.update_available) {
    toast("没有可更新的版本", "warn");
    return;
  }
  if (!(await uiConfirm(
    `将主控从 v${info.current} 更新到 v${info.latest}，过程中面板会短暂中断并自动重启。是否继续？`,
    "一键更新"
  ))) return;
  const btn = $("#btn-upd-run");
  const msg = $("#upd-msg");
  try {
    if (btn) { btn.disabled = true; btn.textContent = "更新中…"; }
    if (msg) msg.textContent = "正在下载并替换二进制，请稍候…";
    const r = await api("/api/system/update", { method: "POST", body: "{}" });
    toast(r.message || "更新完成，即将重启", "ok");
    if (msg) msg.textContent = (r.message || "更新成功") + " 正在等待服务恢复…";
    // 轮询直到服务恢复
    let ok = false;
    for (let i = 0; i < 30; i++) {
      await new Promise((res) => setTimeout(res, 1500));
      try {
        const m = await fetch("/api/meta").then((x) => x.json());
        if (m.version) {
          ok = true;
          state.meta = m;
          if ($("#ver")) $("#ver").textContent = "v" + m.version;
          if ($("#upd-current")) $("#upd-current").textContent = "v" + m.version;
          toast("服务已恢复 · v" + m.version, "ok");
          if (msg) msg.textContent = "更新完成，当前版本 v" + m.version;
          break;
        }
      } catch { /* still restarting */ }
    }
    if (!ok) {
      toast("服务可能仍在重启，请稍后手动刷新页面", "warn");
      if (msg) msg.textContent = "若页面长时间无响应，请 SSH 执行: systemctl status bpanel-master";
    } else {
      await checkUpdate({ quiet: true });
    }
  } catch (e) {
    toast(e.message, "err");
    if (msg) msg.textContent = "更新失败: " + e.message;
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = "一键更新"; }
  }
}

$("#btn-upd-check") && ($("#btn-upd-check").onclick = () => checkUpdate({ quiet: false }));
$("#btn-upd-run") && ($("#btn-upd-run").onclick = () => runSelfUpdate());
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
  a.download = "bpanel-backup.json";
  a.click();
};

boot().catch((e) => {
  $("#auth-err").textContent = e.message;
  showAuth();
});
