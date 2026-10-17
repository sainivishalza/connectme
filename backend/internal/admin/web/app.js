// ConnectMe moderation dashboard. No framework, no build step, no innerHTML:
// all user-supplied text is inserted with textContent, so reported content can
// never execute in a moderator's browser (the CSP forbids inline script too).

const $app = document.getElementById('app');
const state = { access: null, role: null, tab: 'queue', status: 'open', selected: null };

// ---- tiny DOM helper ---------------------------------------------------
function h(tag, props = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) {
    if (kid === null || kid === undefined || kid === false) continue;
    el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return el;
}
const clear = (el) => { while (el.firstChild) el.removeChild(el.firstChild); };
// Appends children, skipping null/false and flattening arrays. (Node.append
// would turn null into the text "null" and an array into "[object ...]".)
const put = (parent, ...kids) => {
  for (const k of kids.flat()) {
    if (k === null || k === undefined || k === false) continue;
    parent.append(k instanceof Node ? k : String(k));
  }
};
const when = (iso) => (iso ? new Date(iso).toLocaleString() : '');
// Auto-flags are identified by their reason. A missing reporter on a user
// report just means that person has since deleted their account.
function sourceOf(r) {
  if (r.reason === 'auto_review') return 'Automatic flag';
  return r.reporter_ref ? 'User report' : 'User report (reporter account deleted)';
}

// ---- session & API -----------------------------------------------------
function decodeRole(token) {
  try {
    const p = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
    return p.role || null;
  } catch { return null; }
}

function setSession(access, refresh) {
  state.access = access;
  state.role = access ? decodeRole(access) : null;
  if (refresh) sessionStorage.setItem('cm_refresh', refresh); // cleared when the tab closes
  if (!access) sessionStorage.removeItem('cm_refresh');
}

async function refreshSession() {
  const rt = sessionStorage.getItem('cm_refresh');
  if (!rt) return false;
  const r = await fetch('/auth/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: rt }) });
  if (!r.ok) { setSession(null); return false; }
  const j = await r.json();
  setSession(j.access_token, j.refresh_token);
  return true;
}

async function api(path, { method = 'GET', body, blob = false } = {}, retry = true) {
  const r = await fetch(path, {
    method,
    headers: { ...(state.access ? { Authorization: `Bearer ${state.access}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (r.status === 401 && retry && await refreshSession()) return api(path, { method, body, blob }, false);
  if (r.status === 401) { signOut(); throw new Error('Session expired'); }
  if (!r.ok) {
    let msg = `Request failed (${r.status})`;
    try { msg = (await r.json()).error || msg; } catch { /* keep default */ }
    throw new Error(msg);
  }
  if (blob) return r.blob();
  return r.status === 204 ? null : r.json();
}

function signOut() {
  setSession(null);
  render();
}

// ---- dialogs ----------------------------------------------------------
function confirmDialog({ title, body, confirmText, danger, withReason }) {
  return new Promise((resolve) => {
    const reason = withReason ? h('textarea', { placeholder: 'Reason (5-300 characters, recorded in the audit log)', maxlength: 300 }) : null;
    const ok = h('button', { class: `btn ${danger ? 'danger' : 'primary'}`, text: confirmText, disabled: !!withReason });
    if (reason) reason.addEventListener('input', () => { ok.disabled = reason.value.trim().length < 5; });
    const dlg = h('dialog', {}, h('h2', { text: title }), h('p', { text: body }), reason,
      h('div', { class: 'row spread' },
        h('button', { class: 'btn', text: 'Cancel', onclick: () => { dlg.close(); resolve(null); } }), ok));
    ok.addEventListener('click', () => { dlg.close(); resolve(reason ? reason.value.trim() : true); });
    dlg.addEventListener('close', () => { dlg.remove(); });
    document.body.append(dlg);
    dlg.showModal();
  });
}

// ---- views --------------------------------------------------------------
function renderLogin(error) {
  clear($app);
  const email = h('input', { type: 'email', autocomplete: 'username', required: true });
  const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const err = h('div', { class: 'error', text: error || '' });
  const form = h('form', { class: 'panel login' },
    h('h2', { text: 'Moderation sign in' }),
    h('label', { text: 'Email' }), email,
    h('label', { text: 'Password' }), pw, err,
    h('p', { class: 'note', text: 'Staff accounts only. Everything you view or do is recorded.' }),
    h('button', { class: 'btn primary', type: 'submit', text: 'Sign in' }));
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.textContent = '';
    try {
      const r = await fetch('/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: email.value.trim(), password: pw.value }) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { err.textContent = j.error || 'Sign in failed'; return; }
      const role = decodeRole(j.access_token);
      if (role !== 'moderator' && role !== 'admin') {
        err.textContent = 'This account does not have moderator access.';
        return;
      }
      setSession(j.access_token, j.refresh_token);
      render();
    } catch { err.textContent = 'Could not reach the server.'; }
  });
  $app.append(form);
}

async function renderShell() {
  clear($app);
  const main = h('main');
  const tabs = [['queue', 'Queue'], ['reports', 'Reports'], ['users', 'Look up a user'], ['live', 'Live streams']];
  if (state.role === 'admin') tabs.push(['audit', 'Audit log']);
  const nav = h('nav', { role: 'tablist' }, tabs.map(([id, label]) =>
    h('button', { role: 'tab', 'aria-selected': String(state.tab === id), text: label, onclick: () => { state.tab = id; state.selected = null; renderShell(); } })));
  $app.append(
    h('header', {}, h('h1', { text: 'ConnectMe Moderation' }), nav, h('div', { class: 'spacer' }),
      h('span', { class: 'badge', text: state.role }), h('button', { class: 'btn', text: 'Sign out', onclick: signOut })),
    main);
  main.append(h('div', { id: 'stats', class: 'stats' }));
  loadStats();
  const body = h('div');
  main.append(body);
  if (state.tab === 'queue') renderQueue(body);
  else if (state.tab === 'reports') renderReports(body);
  else if (state.tab === 'users') renderUserLookup(body);
  else if (state.tab === 'live') renderLive(body);
  else renderAudit(body);
}

async function loadStats() {
  const el = document.getElementById('stats');
  try {
    const s = await api('/moderation/stats');
    clear(el); // refreshed after actions: replace, never append
    const cell = (v, label) => h('div', { class: 'stat' }, h('b', { text: String(v) }), h('span', { text: label }));
    el.append(cell(s.open_reports, 'Open reports'), cell(s.oldest_open_days + ' d', 'Oldest open'),
      cell(s.auto_flags_24h, 'Auto-flags (24 h)'), cell(s.user_reports_24h, 'User reports (24 h)'),
      cell(s.held_videos, 'Videos held'), cell(s.banned_users, 'Banned users'), cell(s.staff_actions_today, 'Staff actions today'));
  } catch (e) { el.append(h('div', { class: 'error', text: e.message })); }
}


// ---- queue -------------------------------------------------------------
// One list for everything waiting on a moderator, oldest first: open reports
// and videos held by the scanner. Reports open in the Reports tab; videos can
// be watched, approved or removed right here.
function renderQueue(root) {
  const panel = h('div', { class: 'panel' });
  root.append(panel);
  async function load() {
    clear(panel);
    panel.append(h('div', { class: 'row spread' }, h('h2', { text: 'Review queue' }),
      h('button', { class: 'btn', text: 'Refresh', onclick: () => { load(); loadStats(); } })));
    let items;
    try { ({ items } = await api('/moderation/queue')); }
    catch (e) { panel.append(h('div', { class: 'error', text: e.message })); return; }
    if (!items.length) { panel.append(h('div', { class: 'empty', text: 'Nothing is waiting. Queue is clear.' })); return; }
    const rows = h('div');
    panel.append(rows);
    for (const it of items) {
      const slot = h('div', { class: 'evidence' });
      slot.hidden = true;
      const actions = h('div', { class: 'row' });
      if (it.type === 'video') {
        actions.append(
          h('button', { class: 'btn', text: 'Watch', onclick: async () => {
            try {
              const { url } = await api(`/moderation/videos/${encodeURIComponent(it.id)}/ticket`);
              clear(slot);
              slot.append(h('video', { src: url, controls: true, playsinline: true, width: 320 }));
              slot.hidden = false;
            } catch (e) { clear(slot); slot.append(h('div', { class: 'error', text: e.message })); slot.hidden = false; }
          } }),
          h('button', { class: 'btn primary', text: 'Approve', onclick: async () => {
            try { await api(`/moderation/videos/${encodeURIComponent(it.id)}/approve`, { method: 'POST' }); load(); loadStats(); }
            catch (e) { clear(slot); slot.append(h('div', { class: 'error', text: e.message })); slot.hidden = false; }
          } }),
          h('button', { class: 'btn danger', text: 'Remove', onclick: async () => {
            const reason = await confirmDialog({ title: 'Remove this video?', body: 'The file is deleted for good. Provide a reason; it is stored in the audit log.',
              confirmText: 'Remove video', danger: true, withReason: true });
            if (!reason) return;
            try { await api(`/moderation/videos/${encodeURIComponent(it.id)}/remove`, { method: 'POST', body: { reason } }); load(); loadStats(); }
            catch (e) { clear(slot); slot.append(h('div', { class: 'error', text: e.message })); slot.hidden = false; }
          } }));
      } else {
        actions.append(h('button', { class: 'btn', text: 'Open report', onclick: () => { state.tab = 'reports'; state.selected = it.id; renderShell(); } }));
      }
      const source = { user_report: 'User report', automatic: 'Automatic flag', scanner: 'Video scanner' }[it.source] || it.source;
      rows.append(h('div', { class: 'item' },
        h('div', { class: 'row spread' }, h('b', { text: it.type === 'video' ? 'Video held for review' : 'Report' }),
          h('span', { class: `badge ${it.source === 'user_report' ? '' : 'auto'}`, text: source })),
        h('small', { text: `${when(it.created_at)} · ${it.summary.slice(0, 140)}${it.seconds ? ` · ${it.seconds} s` : ''}` }),
        actions, slot));
    }
  }
  load();
}

// ---- reports -----------------------------------------------------------
function renderReports(root) {
  const list = h('div'); const detail = h('div', { class: 'panel' }, h('div', { class: 'empty', text: 'Select a report' }));
  const status = h('select', { 'aria-label': 'Status' }, ['open', 'reviewing', 'actioned', 'dismissed'].map((s) =>
    h('option', { value: s, text: s, selected: s === state.status })));
  status.addEventListener('change', () => { state.status = status.value; load(); });
  root.append(h('div', { class: 'grid' },
    h('div', { class: 'panel' }, h('div', { class: 'row spread' }, h('h2', { text: 'Reports' }), status), list), detail));

  async function load() {
    clear(list);
    list.append(h('div', { class: 'empty', text: 'Loading…' }));
    try {
      const { reports } = await api(`/moderation/reports?status=${encodeURIComponent(state.status)}&limit=50`);
      clear(list);
      if (!reports.length) { list.append(h('div', { class: 'empty', text: 'Nothing here. Queue is clear.' })); return; }
      for (const r of reports) {
        const item = h('div', { class: 'item', 'aria-selected': String(state.selected === r.id), onclick: () => { state.selected = r.id; showDetail(r); load(); } },
          h('div', { class: 'row spread' },
            h('b', { text: r.reason.replace(/_/g, ' ') }),
            h('span', {}, r.reason === 'auto_review' ? h('span', { class: 'badge auto', text: 'auto-flag' }) : null,
              r.reported_banned ? h('span', { class: 'badge banned', text: 'banned' }) : null)),
          h('small', { text: `${when(r.created_at)} · ${r.details.slice(0, 90)}` }));
        list.append(item);
        if (state.selected === r.id) showDetail(r);
      }
    } catch (e) { clear(list); list.append(h('div', { class: 'error', text: e.message })); }
  }

  function showDetail(r) {
    clear(detail);
    const resolve = async (action) => {
      const ok = await confirmDialog({
        title: action === 'ban' ? 'Ban this user?' : 'Dismiss this report?',
        body: action === 'ban' ? 'They are signed out immediately and cannot sign in. This is recorded in the audit log.' : 'The report is closed with no action.',
        confirmText: action === 'ban' ? 'Ban user' : 'Dismiss', danger: action === 'ban',
      });
      if (!ok) return;
      try { await api(`/moderation/reports/${r.id}/resolve`, { method: 'POST', body: { action } }); state.selected = null; renderShell(); }
      catch (e) { detail.append(h('div', { class: 'error', text: e.message })); }
    };
    const open = r.status === 'open' || r.status === 'reviewing';
    put(detail,
      h('h2', { text: 'Report' }),
      h('dl', { class: 'kv' },
        h('dt', { text: 'Reason' }), h('dd', { text: r.reason }),
        h('dt', { text: 'Reported' }), h('dd', { text: when(r.created_at) }),
        h('dt', { text: 'Source' }), h('dd', { text: sourceOf(r) }),
        h('dt', { text: 'Status' }), h('dd', { text: r.status }),
        h('dt', { text: 'Details' }), h('dd', { text: r.details || '(none)' })),
      h('h2', { text: 'Evidence' }),
      h('div', {}, r.evidence && r.evidence.length ? r.evidence.map((e) => h('div', { class: 'evidence', text: e })) : h('div', { class: 'note', text: 'No evidence attached.' })),
      h('div', { class: 'row', style: 'margin-top:14px' },
        h('button', { class: 'btn', text: 'Open user', onclick: () => { state.tab = 'users'; state.userRef = r.reported_ref; renderShell(); } }),
        open ? h('button', { class: 'btn', text: 'Dismiss', onclick: () => resolve('dismiss') }) : null,
        open ? h('button', { class: 'btn danger', text: 'Ban user', onclick: () => resolve('ban') }) : null));
  }
  load();
}

// ---- user lookup -------------------------------------------------------
function renderUserLookup(root) {
  const input = h('input', { placeholder: 'Profile code (ref)', style: 'min-width:340px', value: state.userRef || '' });
  const out = h('div', { class: 'panel', style: 'margin-top:16px' }, h('div', { class: 'empty', text: 'Enter a profile code or open a user from a report.' }));
  const go = () => { state.userRef = input.value.trim(); if (state.userRef) loadUser(); };
  root.append(h('div', { class: 'row' }, input, h('button', { class: 'btn primary', text: 'View', onclick: go })), out);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });

  async function loadUser() {
    clear(out); out.append(h('div', { class: 'empty', text: 'Loading…' }));
    try {
      const u = await api(`/moderation/users/${encodeURIComponent(state.userRef)}`);
      clear(out);
      const photos = h('div', { class: 'photos' });
      if (u.open_reports > 0) {
        for (const id of u.photo_ids) {
          api(`/moderation/users/${encodeURIComponent(u.ref)}/photos/${id}`, { blob: true })
            .then((b) => photos.append(h('img', { src: URL.createObjectURL(b), alt: 'Profile photo' })))
            .catch(() => {});
        }
      }
      const ban = async (doBan) => {
        const reason = await confirmDialog({
          title: doBan ? 'Ban this user?' : 'Lift this ban?', body: 'Provide a reason. It is stored in the audit log.',
          confirmText: doBan ? 'Ban user' : 'Unban', danger: doBan, withReason: true,
        });
        if (!reason) return;
        try { await api(`/moderation/users/${encodeURIComponent(u.ref)}/${doBan ? 'ban' : 'unban'}`, { method: 'POST', body: { reason } }); loadUser(); loadStats(); }
        catch (e) { out.append(h('div', { class: 'error', text: e.message })); }
      };
      put(out,
        h('div', { class: 'row spread' }, h('h2', { text: u.display_name || '(no name)' }),
          h('span', {}, u.banned ? h('span', { class: 'badge banned', text: 'banned' }) : h('span', { class: 'badge ok', text: 'active' }),
            ' ', h('span', { class: 'badge', text: u.role }))),
        h('dl', { class: 'kv' },
          h('dt', { text: 'Age' }), h('dd', { text: String(u.age) }),
          h('dt', { text: 'Mode' }), h('dd', { text: u.mode }),
          h('dt', { text: 'Intro' }), h('dd', { text: u.intro || '(none)' }),
          h('dt', { text: 'Bio' }), h('dd', { text: u.bio || '(none)' }),
          h('dt', { text: 'Mood' }), h('dd', { text: u.mood || '(none)' }),
          h('dt', { text: 'Tags' }), h('dd', { text: u.tags.join(', ') || '(none)' }),
          h('dt', { text: 'Joined' }), h('dd', { text: when(u.created_at) }),
          h('dt', { text: 'Verified' }), h('dd', { text: `email ${u.email_verified ? 'yes' : 'no'} · ID ${u.id_verified ? 'yes' : 'no'}` }),
          h('dt', { text: 'Activity' }), h('dd', { text: `${u.matches} matches · ${u.groups} groups` }),
          h('dt', { text: 'Reports' }), h('dd', { text: `${u.reports_about} against (${u.open_reports} open) · ${u.reports_filed} filed` })),
        u.photo_ids.length ? h('div', {}, h('b', { text: `Photos (${u.photo_ids.length})` }),
          u.open_reports > 0 ? photos : h('div', { class: 'note', text: 'Photos can be reviewed only while the user has an open report.' })) : null,
        h('p', { class: 'note', text: 'Email, phone, date of birth, location and chat content are not available here.' }),
        h('div', { class: 'row' },
          u.banned ? h('button', { class: 'btn', text: 'Unban', onclick: () => ban(false) }) : h('button', { class: 'btn danger', text: 'Ban user', onclick: () => ban(true) })),
        h('h2', { style: 'margin-top:18px', text: 'Recent reports' }),
        u.recent_reports.length ? h('table', {}, h('tr', {}, ['When', 'Reason', 'Status', 'Details'].map((c) => h('th', { text: c }))),
          u.recent_reports.map((r) => h('tr', {}, h('td', { text: when(r.created_at) }), h('td', { text: r.reason }), h('td', { text: r.status }), h('td', { text: r.details.slice(0, 120) }))))
          : h('div', { class: 'note', text: 'None.' }));
    } catch (e) { clear(out); out.append(h('div', { class: 'error', text: e.message })); }
  }
  if (state.userRef) loadUser();
}

// ---- audit log ----------------------------------------------------------
function renderLive(root) {
  const panel = h('div', { class: 'panel' });
  root.append(panel);
  async function load() {
    clear(panel);
    panel.append(h('div', { class: 'row spread' }, h('h2', { text: 'Live streams' }),
      h('button', { class: 'btn', text: 'Refresh', onclick: load })));
    let rooms;
    try { ({ rooms } = await api('/live/rooms')); }
    catch (e) { panel.append(h('div', { class: 'error', text: e.message })); return; }
    if (!rooms.length) { panel.append(h('div', { class: 'empty', text: 'Nobody is live.' })); return; }
    panel.append(h('table', {}, h('tr', {}, ['Title', 'Host', 'Started', ''].map((c) => h('th', { text: c }))),
      rooms.map((r) => h('tr', {}, h('td', { text: r.title }), h('td', { text: r.host_name || r.host_ref.slice(0, 8) }),
        h('td', { text: when(r.started_at) }),
        h('td', {}, h('button', { class: 'btn danger', text: 'End stream', onclick: async () => {
          const reason = await confirmDialog({ title: 'End this stream?', body: 'The host and viewers are disconnected. Provide a reason; it is stored in the audit log.',
            confirmText: 'End stream', danger: true, withReason: true });
          if (!reason) return;
          try { await api(`/moderation/live/${encodeURIComponent(r.id)}/end`, { method: 'POST', body: { reason } }); load(); loadStats(); }
          catch (e) { panel.append(h('div', { class: 'error', text: e.message })); }
        } }))))));
  }
  load();
}

function renderAudit(root) {
  const panel = h('div', { class: 'panel' }, h('h2', { text: 'Staff audit log' }), h('div', { class: 'empty', text: 'Loading…' }));
  root.append(panel);
  api('/moderation/audit?limit=100').then(({ entries }) => {
    clear(panel);
    panel.append(h('h2', { text: 'Staff audit log' }), h('p', { class: 'note', text: 'Append-only. Opening this page is itself recorded.' }),
      h('table', {}, h('tr', {}, ['When', 'Staff', 'Action', 'Target', 'Detail'].map((c) => h('th', { text: c }))),
        entries.map((e) => h('tr', {}, h('td', { text: when(e.at) }), h('td', { text: `${e.actor_role} ${e.actor_ref.slice(0, 8)}` }),
          h('td', { text: e.action }), h('td', { text: e.target_ref ? e.target_ref.slice(0, 8) : '' }), h("td", { text: e.detail && Object.keys(e.detail).length ? JSON.stringify(e.detail) : "" })))));
  }).catch((e) => { clear(panel); panel.append(h('div', { class: 'error', text: e.message })); });
}

// ---- boot ---------------------------------------------------------------
function render() {
  if (!state.access) renderLogin(); else renderShell();
}
(async () => {
  if (await refreshSession()) { if (state.role === 'moderator' || state.role === 'admin') { render(); return; } setSession(null); }
  render();
})();
