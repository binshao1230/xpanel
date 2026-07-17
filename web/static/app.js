const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];
const state = {
  token: localStorage.getItem("xpanel_token") || "",
  meta: null,
  servers: [],
  plans: [],
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

function setTheme(t) {
  document.documentElement.setAttribute("data-theme", t);
  localStorage.setItem("xpanel_theme", t);
}

async function boot() {
  setTheme(localStorage.getItem("xpanel_theme") || "dark");
  const meta = await api("/api/meta");
  state.meta = meta;
  $("#ver").textContent = "v" + meta.version;
  if (!meta.initialized) {
    $("#auth-hint").textContent = "首次启动：创建管理员";
    $("#auth-btn").textContent = "创建并登录";
    showAuth();
    return;
  }
  if (!state.token) {
    $("#auth-hint").textContent = "登录主控";
    $("#auth-btn").textContent = "登录";
    showAuth();
    return;
  }
  try {
    await enterMain();
  } catch {
    state.token = "";
    localStorage.removeItem("xpanel_token");
    showAuth();
  }
}

function showAuth() {
  $("#view-auth").classList.remove("hidden");
  $("#view-main").classList.add("hidden");
}

async function enterMain() {
  $("#view-auth").classList.add("hidden");
  $("#view-main").classList.remove("hidden");
  const me = await api("/api/auth/me");
  $("#nav-user").textContent = me.user.username + " · " + me.user.role;
  const o = location.origin;
  $("#sub-url").textContent = (me.subscribe_url?.startsWith("http") ? me.subscribe_url : o + "/sub/" + me.user.subscribe_token);
  $("#sub-clash").textContent = (me.subscribe_clash?.startsWith("http") ? me.subscribe_clash : o + "/sub/" + me.user.subscribe_token + "/clash");
  $("#sub-singbox").textContent = (me.subscribe_singbox?.startsWith("http") ? me.subscribe_singbox : o + "/sub/" + me.user.subscribe_token + "/singbox");
  await refreshDash();
  switchTab("dash");
}

$("#auth-btn").onclick = async () => {
  $("#auth-err").textContent = "";
  try {
    const path = state.meta?.initialized ? "/api/auth/login" : "/api/auth/setup";
    const data = await api(path, {
      method: "POST",
      body: JSON.stringify({ username: $("#username").value.trim(), password: $("#password").value }),
    });
    state.token = data.token;
    localStorage.setItem("xpanel_token", state.token);
    await enterMain();
  } catch (e) {
    $("#auth-err").textContent = e.message;
  }
};

$$("#nav .nav").forEach((btn) => {
  btn.onclick = () => switchTab(btn.dataset.tab);
});

function switchTab(name) {
  $$("#nav .nav").forEach((b) => b.classList.toggle("active", b.dataset.tab === name));
  $$(".tab").forEach((t) => t.classList.add("hidden"));
  const el = $("#tab-" + name);
  if (el) el.classList.remove("hidden");
  const loaders = {
    dash: refreshDash,
    servers: refreshServers,
    inbounds: async () => { await fillServerSelects(); await fillCertSelect(); await refreshInbounds(); },
    certs: async () => { await fillServerSelects(); await refreshCerts(); },
    outbounds: async () => { await fillServerSelects(); await refreshOutbounds(); await refreshRoutes(); },
    plans: refreshPlans,
    users: async () => { await refreshPlans(); await refreshUsers(); },
    nodes: refreshExt,
    traffic: refreshTraffic,
    speed: () => Promise.resolve(),
    settings: refreshSettings,
  };
  if (loaders[name]) loaders[name]().catch((e) => alert(e.message));
}

async function refreshDash() {
  const d = await api("/api/dashboard");
  $("#dash-stats").innerHTML = [
    ["用户", d.users],
    ["服务器", d.servers],
    ["在线", d.online],
    ["入站", d.inbounds],
    ["套餐", d.plans],
    ["上行", fmtBytes(d.traffic_up)],
    ["下行", fmtBytes(d.traffic_down)],
  ].map(([l, n]) => `<div class="stat"><div class="n">${n}</div><div class="l">${l}</div></div>`).join("");
}

async function fillServerSelects() {
  const data = await api("/api/servers");
  state.servers = data.servers || [];
  for (const id of ["in-server", "ob-server", "rt-server", "acme-server"]) {
    const sel = $("#" + id);
    if (!sel) continue;
    const head = id === "acme-server" ? '<option value="">下发全部 Agent</option>' : "";
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
      `<option value="${c.id}">${escapeHtml(c.domain)} (#${c.id})</option>`
    ).join("");
}

async function refreshServers() {
  const data = await api("/api/servers");
  state.servers = data.servers || [];
  const box = $("#server-list");
  box.innerHTML = "";
  for (const s of state.servers) {
    const dot = s.online ? "on" : s.status === "pending" ? "pending" : "off";
    const el = document.createElement("div");
    el.className = "item";
    el.innerHTML = `<div>
      <div><span class="dot ${dot}"></span><strong>${escapeHtml(s.name)}</strong></div>
      <div class="meta">${escapeHtml(s.hostname||"-")} · ${escapeHtml(s.public_ip||"no-ip")} · cfg v${s.config_version}
      · ${s.xray_running?"xray:on":"xray:off"} · ↑${fmtBytes(s.traffic_up)} ↓${fmtBytes(s.traffic_down)}</div>
    </div>
    <div class="row">
      <button class="small" data-act="install" data-id="${s.id}">安装命令</button>
      <button class="small danger" data-act="del" data-id="${s.id}">删除</button>
    </div>`;
    box.appendChild(el);
  }
  if (!state.servers.length) box.innerHTML = '<p class="muted">暂无服务器</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    if (btn.dataset.act === "del") {
      if (!confirm("删除服务器？")) return;
      await api("/api/servers/" + btn.dataset.id, { method: "DELETE" });
      await refreshServers();
    }
    if (btn.dataset.act === "install") {
      const info = await api("/api/servers/" + btn.dataset.id + "/install-cmd");
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
  await api("/api/servers", { method: "POST", body: JSON.stringify({ name }) });
  $("#server-name").value = "";
  await refreshServers();
};

async function refreshInbounds() {
  const data = await api("/api/inbounds");
  const box = $("#inbound-list");
  box.innerHTML = (data.inbounds || []).map((inb) => `
    <div class="item"><div>
      <strong>${escapeHtml(inb.tag)}</strong> · ${escapeHtml(inb.protocol)} :${inb.port}
      <div class="meta">server ${escapeHtml(inb.server_id.slice(0,8))}… · x${inb.multiplier||1}</div>
    </div><button class="small danger" data-id="${inb.id}">删除</button></div>`).join("") || '<p class="muted">暂无入站</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    if (!confirm("删除入站？")) return;
    await api("/api/inbounds/" + btn.dataset.id, { method: "DELETE" });
    await refreshInbounds();
  };
}

$("#btn-add-in").onclick = async () => {
  const cert_id = Number($("#in-cert").value) || 0;
  await api("/api/inbounds", {
    method: "POST",
    body: JSON.stringify({
      server_id: $("#in-server").value,
      protocol: $("#in-proto").value,
      port: Number($("#in-port").value),
      tag: $("#in-tag").value.trim() || undefined,
      cert_id,
      enable_tls: cert_id > 0,
    }),
  });
  await refreshInbounds();
};

$("#btn-reality").onclick = async () => {
  const server_id = $("#in-server").value;
  if (!server_id) return alert("先选服务器");
  const r = await api("/api/inbounds/quick-reality", {
    method: "POST",
    body: JSON.stringify({ server_id, port: Number($("#in-port").value) || 443 }),
  });
  alert("已创建 Reality 入站 id=" + r.id + "\n" + r.note);
  await refreshInbounds();
};

async function refreshOutbounds() {
  const data = await api("/api/outbounds");
  const box = $("#ob-list");
  box.innerHTML = (data.outbounds || []).map((o) => `
    <div class="item"><div><strong>${escapeHtml(o.tag)}</strong> · ${escapeHtml(o.protocol)}
    <div class="meta">${escapeHtml(o.server_id.slice(0,8))}…</div></div>
    <button class="small danger" data-id="${o.id}">删除</button></div>`).join("") || '<p class="muted">暂无自定义出站</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/outbounds/" + btn.dataset.id, { method: "DELETE" });
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
  $("#ob-tag").value = "";
  await refreshOutbounds();
};

async function refreshRoutes() {
  const data = await api("/api/routes");
  const box = $("#rt-list");
  box.innerHTML = (data.routes || []).map((r) => `
    <div class="item"><div><strong>${escapeHtml(r.name)}</strong> → ${escapeHtml(r.outbound_tag)}
    <div class="meta">${escapeHtml(r.domain_json)}</div></div>
    <button class="small danger" data-id="${r.id}">删除</button></div>`).join("") || '<p class="muted">暂无路由</p>';
  box.onclick = async (e) => {
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
      domain,
      name: $("#rt-out").value.trim(),
    }),
  });
  await refreshRoutes();
};

async function refreshPlans() {
  const data = await api("/api/plans");
  state.plans = data.plans || [];
  const sel = $("#u-plan");
  if (sel) {
    sel.innerHTML = '<option value="0">无套餐</option>' +
      state.plans.map((p) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join("");
  }
  const box = $("#plan-list");
  if (!box) return;
  box.innerHTML = state.plans.map((p) => `
    <div class="item"><div><strong>${escapeHtml(p.name)}</strong>
    <div class="meta">流量 ${fmtBytes(p.traffic_limit)} · ${p.duration_days}天 · ${escapeHtml(p.price_note||"")}</div></div>
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
  const box = $("#user-list");
  box.innerHTML = (data.users || []).map((u) => `
    <div class="item"><div>
      <strong>${escapeHtml(u.username)}</strong> · ${escapeHtml(u.role)} ${u.enabled?"":"(禁用)"}
      <div class="meta">流量 ${fmtBytes(u.traffic_used)} / ${u.traffic_limit?fmtBytes(u.traffic_limit):"∞"}
      · 到期 ${u.expire_at?new Date(u.expire_at*1000).toLocaleDateString():"-"} · plan ${u.plan_id||0}</div>
    </div>
    <div class="row">
      <button class="small" data-act="renew" data-id="${u.id}">续期30天</button>
      <button class="small" data-act="reset" data-id="${u.id}">重置流量</button>
      <button class="small danger" data-act="del" data-id="${u.id}">删除</button>
    </div></div>`).join("") || '<p class="muted">暂无用户</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    const id = btn.dataset.id;
    if (btn.dataset.act === "del") {
      if (!confirm("删除用户？")) return;
      await api("/api/users/" + id, { method: "DELETE" });
    }
    if (btn.dataset.act === "renew") {
      await api("/api/users/" + id, { method: "PUT", body: JSON.stringify({ renew_days: 30 }) });
    }
    if (btn.dataset.act === "reset") {
      await api("/api/users/" + id, { method: "PUT", body: JSON.stringify({ reset_traffic: true }) });
    }
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

async function refreshExt() {
  const data = await api("/api/nodes/external");
  const box = $("#ext-list");
  box.innerHTML = (data.nodes || []).map((n) => `
    <div class="item"><div><strong>${escapeHtml(n.name)}</strong> · ${escapeHtml(n.protocol)}
    <div class="meta">${escapeHtml(n.address)}:${n.port}</div></div>
    <button class="small danger" data-id="${n.id}">删除</button></div>`).join("") || '<p class="muted">暂无外部节点</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button[data-id]");
    if (!btn) return;
    await api("/api/nodes/external/" + btn.dataset.id, { method: "DELETE" });
    await refreshExt();
  };
}

$("#btn-import").onclick = async () => {
  const r = await api("/api/nodes/external", {
    method: "POST",
    body: JSON.stringify({ links: $("#ext-links").value }),
  });
  alert("导入 " + r.imported + " 条");
  $("#ext-links").value = "";
  await refreshExt();
};

async function refreshCerts() {
  const data = await api("/api/certs");
  const box = $("#cert-list");
  box.innerHTML = (data.certificates || []).map((c) => {
    const exp = c.expire_at ? new Date(c.expire_at * 1000).toLocaleString() : "-";
    return `<div class="item"><div>
      <strong>${escapeHtml(c.name || c.domain)}</strong> · <span class="meta">${escapeHtml(c.status || "active")}</span>
      <div class="meta">${escapeHtml(c.domain)} · ${escapeHtml(c.provider || c.challenge || "manual")}
      · 到期 ${exp}${c.auto_renew ? " · 自动续期" : ""}${c.server_id ? " · 绑定 " + escapeHtml(c.server_id.slice(0,8)) : " · 全部Agent"}
      ${c.last_error ? " · err: " + escapeHtml(c.last_error) : ""}</div>
    </div>
    <div class="row">
      <button class="small" data-act="deploy" data-id="${c.id}">下发Agent</button>
      <button class="small" data-act="renew" data-id="${c.id}">续期</button>
      <button class="small danger" data-act="del" data-id="${c.id}">删除</button>
    </div></div>`;
  }).join("") || '<p class="muted">暂无证书</p>';
  box.onclick = async (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    if (btn.dataset.act === "del") {
      await api("/api/certs/" + btn.dataset.id, { method: "DELETE" });
    }
    if (btn.dataset.act === "deploy") {
      await api("/api/certs/" + btn.dataset.id + "/deploy", { method: "POST", body: "{}" });
      alert("已触发下发（Agent 下次心跳会拉取）");
    }
    if (btn.dataset.act === "renew") {
      btn.disabled = true;
      try {
        await api("/api/certs/" + btn.dataset.id + "/renew", { method: "POST", body: "{}" });
        alert("续期并下发成功");
      } catch (err) {
        alert(err.message);
      }
      btn.disabled = false;
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
  btn.textContent = "申请中…";
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
    alert("申请成功 id=" + r.id + " 到期 " + new Date(r.expire_at * 1000).toLocaleString() + "\n已触发 Agent 下发");
    await refreshCerts();
  } catch (e) {
    alert("申请失败: " + e.message);
    await refreshCerts();
  }
  btn.disabled = false;
  btn.textContent = "申请并下发";
};

async function refreshTraffic() {
  const t = await api("/api/traffic");
  $("#traffic-box").textContent =
    `服务器累计 ↑ ${fmtBytes(t.server_up)}  ↓ ${fmtBytes(t.server_down)}\n用户已用合计 ${fmtBytes(t.user_used)}`;
  $("#traffic-daily").innerHTML = (t.daily || []).slice().reverse().map((d) => `
    <div class="item"><div>${escapeHtml(d.day)} · server ${escapeHtml(d.server_id||"-").slice(0,8)}
    <div class="meta">↑${fmtBytes(d.up)} ↓${fmtBytes(d.down)}</div></div></div>`).join("") || '<p class="muted">暂无日统计</p>';
}

async function refreshSettings() {
  const data = await api("/api/settings");
  const s = data.settings || {};
  $("#set-site").value = s.site_name || "XPanel";
  $("#set-theme").value = s.theme || "dark";
  $("#set-tg").value = s.tg_bot_token || "";
  $("#set-acme-email").value = s.acme_email || "";
  $("#set-cf-token").value = s.cf_dns_api_token || "";
  $("#set-dns-key").value = s.dns_api_key || "";
  $("#set-dns-secret").value = s.dns_api_secret || "";
  $("#set-webhook").value = s.webhook_url || "";
  if (s.acme_email && !$("#acme-email").value) $("#acme-email").value = s.acme_email;
}

$("#btn-save-set").onclick = async () => {
  await api("/api/settings", {
    method: "PUT",
    body: JSON.stringify({
      site_name: $("#set-site").value.trim(),
      theme: $("#set-theme").value,
      tg_bot_token: $("#set-tg").value.trim(),
      acme_email: $("#set-acme-email").value.trim(),
      cf_dns_api_token: $("#set-cf-token").value.trim(),
      dns_api_key: $("#set-dns-key").value.trim(),
      dns_api_secret: $("#set-dns-secret").value.trim(),
      webhook_url: $("#set-webhook").value.trim(),
    }),
  });
  setTheme($("#set-theme").value);
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

$("#btn-st-one").onclick = async () => {
  const r = await api("/api/speedtest", {
    method: "POST",
    body: JSON.stringify({
      host: $("#st-host").value.trim(),
      port: Number($("#st-port").value),
      tls: $("#st-tls").checked,
    }),
  });
  $("#speed-list").innerHTML = `<div class="item"><div><strong>${escapeHtml(r.target)}</strong>
    <div class="meta">TCP ${r.tcp_ms?.toFixed?.(2) ?? r.tcp_ms} ms ${r.tls_ms ? "· TLS " + r.tls_ms.toFixed(2) + " ms" : ""}
    ${r.error ? "· " + escapeHtml(r.error) : "· ok"}</div></div></div>`;
};

$("#btn-st-batch").onclick = async () => {
  const data = await api("/api/speedtest/batch", { method: "POST", body: "{}" });
  $("#speed-list").innerHTML = (data.results || []).map((x) => {
    const r = x.result || {};
    return `<div class="item"><div><strong>${escapeHtml(x.server)} / ${escapeHtml(x.tag)}</strong>
      <div class="meta">${escapeHtml(r.target || "")} · TCP ${r.tcp_ms?.toFixed?.(2) ?? "-"} ms
      ${r.error ? "· " + escapeHtml(r.error) : "· ok"}</div></div></div>`;
  }).join("") || '<p class="muted">无节点</p>';
};

$("#btn-theme").onclick = () => {
  const cur = document.documentElement.getAttribute("data-theme") || "dark";
  setTheme(cur === "dark" ? "light" : "dark");
};
$("#btn-logout").onclick = () => {
  localStorage.removeItem("xpanel_token");
  location.reload();
};
$("#modal-close").onclick = () => $("#modal").classList.add("hidden");

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}

boot().catch((e) => {
  $("#auth-err").textContent = e.message;
  showAuth();
});
