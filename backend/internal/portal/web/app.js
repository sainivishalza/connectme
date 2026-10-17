// ConnectMe partner portal. No framework, no build step, no HTML strings: all
// text from the server (booking notes, guest names) is inserted with
// textContent, and the CSP forbids inline script, so nothing a member types can
// run in a venue manager's browser.

const $app = document.getElementById('app');
const state = { access: null, partners: [], partner: null, tab: 'bookings', status: 'requested' };

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
const when = (iso) => (iso ? new Date(iso).toLocaleString() : '');
const money = (c) => `$${(c / 100).toFixed(2)}`;

// ---- session & API -----------------------------------------------------
function setSession(access, refresh) {
  state.access = access;
  if (refresh) sessionStorage.setItem('cm_partner_refresh', refresh); // cleared when the tab closes
  if (!access) sessionStorage.removeItem('cm_partner_refresh');
}

async function refreshSession() {
  const rt = sessionStorage.getItem('cm_partner_refresh');
  if (!rt) return false;
  const r = await fetch('/auth/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: rt }) });
  if (!r.ok) { setSession(null); return false; }
  const j = await r.json();
  setSession(j.access_token, j.refresh_token);
  return true;
}

async function api(path, { method = 'GET', body } = {}, retry = true) {
  const r = await fetch(path, {
    method,
    headers: { ...(state.access ? { Authorization: `Bearer ${state.access}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (r.status === 401 && retry && await refreshSession()) return api(path, { method, body }, false);
  if (r.status === 401) { signOut(); throw new Error('Session expired'); }
  if (!r.ok) {
    let msg = `Request failed (${r.status})`;
    try { msg = (await r.json()).error || msg; } catch { /* keep default */ }
    throw new Error(msg);
  }
  return r.status === 204 ? null : r.json();
}

function signOut() { setSession(null); state.partners = []; state.partner = null; render(); }

// ---- dialogs -----------------------------------------------------------
function askAmount({ title, body, confirmText, current }) {
  return new Promise((resolve) => {
    const input = h('input', { type: 'number', min: '0.50', step: '0.01', placeholder: 'Price in dollars', value: current ? (current / 100).toFixed(2) : '' });
    const err = h('div', { class: 'error' });
    const ok = h('button', { class: 'btn primary', text: confirmText });
    const dlg = h('dialog', {}, h('h2', { text: title }), h('p', { text: body }), input, err,
      h('div', { class: 'row spread' }, h('button', { class: 'btn', text: 'Cancel', onclick: () => { dlg.close(); resolve(null); } }), ok));
    ok.addEventListener('click', () => {
      const cents = Math.round(parseFloat(input.value) * 100);
      if (!Number.isFinite(cents) || cents < 50) { err.textContent = 'Enter a price of at least $0.50.'; return; }
      dlg.close(); resolve(cents);
    });
    dlg.addEventListener('close', () => dlg.remove());
    document.body.append(dlg);
    dlg.showModal();
  });
}

// ---- views -------------------------------------------------------------
function renderLogin(error) {
  clear($app);
  const email = h('input', { type: 'email', autocomplete: 'username', required: true });
  const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const err = h('div', { class: 'error', text: error || '' });
  const form = h('form', { class: 'panel login' },
    h('h2', { text: 'Partner sign in' }),
    h('label', { text: 'Email' }), email,
    h('label', { text: 'Password' }), pw, err,
    h('p', { class: 'note', text: 'Use the ConnectMe account an administrator linked to your venue.' }),
    h('button', { class: 'btn primary', type: 'submit', text: 'Sign in' }));
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    err.textContent = '';
    try {
      const r = await fetch('/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: email.value.trim(), password: pw.value }) });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) { err.textContent = j.error || 'Sign in failed'; return; }
      setSession(j.access_token, j.refresh_token);
      await loadPartners();
      render();
    } catch { err.textContent = 'Could not reach the server.'; }
  });
  $app.append(form);
}

async function loadPartners() {
  const { partners } = await api('/partners/manage/partners');
  state.partners = partners;
  if (!state.partner || !partners.some((p) => p.id === state.partner)) state.partner = partners[0] ? partners[0].id : null;
}

function render() {
  if (!state.access) { renderLogin(); return; }
  clear($app);
  const current = state.partners.find((p) => p.id === state.partner);
  const picker = state.partners.length > 1
    ? h('select', { 'aria-label': 'Venue', onchange: (e) => { state.partner = e.target.value; render(); } },
        state.partners.map((p) => h('option', { value: p.id, text: p.name, selected: p.id === state.partner })))
    : null;
  const tabs = [['bookings', 'Bookings'], ['events', 'Events'], ['report', 'Earnings']];
  $app.append(
    h('header', {}, h('h1', { text: current ? current.name : 'Partner portal' }), picker,
      h('nav', { role: 'tablist' }, tabs.map(([id, label]) =>
        h('button', { role: 'tab', 'aria-selected': String(state.tab === id), text: label, onclick: () => { state.tab = id; render(); } }))),
      h('div', { class: 'spacer' }), h('button', { class: 'btn', text: 'Sign out', onclick: signOut })));
  const main = h('main');
  $app.append(main);
  if (!current) {
    main.append(h('div', { class: 'panel empty', text: 'Your account does not manage a venue yet. Ask a ConnectMe administrator to link it.' }));
    return;
  }
  if (state.tab === 'bookings') renderBookings(main, current);
  else if (state.tab === 'events') renderEvents(main, current);
  else renderReport(main, current);
}

const statusLabel = { requested: 'Waiting for you', confirmed: 'Confirmed', declined: 'Declined', cancelled: 'Cancelled by guest', completed: 'Completed' };
const payLabel = { unpaid: '', paid: 'Paid through ConnectMe', refunded: 'Refunded' };

function renderBookings(root, partner) {
  const panel = h('div', { class: 'panel' });
  root.append(panel);
  const status = h('select', { 'aria-label': 'Status' }, ['requested', 'confirmed', 'completed', 'declined', 'cancelled', ''].map((s) =>
    h('option', { value: s, text: s ? statusLabel[s] : 'All', selected: s === state.status })));
  status.addEventListener('change', () => { state.status = status.value; load(); });
  async function load() {
    clear(panel);
    panel.append(h('div', { class: 'row spread' }, h('h2', { text: 'Bookings' }), status));
    let bookings;
    try { ({ bookings } = await api(`/partners/manage/partners/${encodeURIComponent(partner.id)}/bookings?status=${encodeURIComponent(state.status)}`)); }
    catch (e) { panel.append(h('div', { class: 'error', text: e.message })); return; }
    if (!bookings.length) { panel.append(h('div', { class: 'empty', text: 'No bookings here.' })); return; }
    for (const b of bookings) {
      const act = async (action, amount) => {
        try { await api(`/partners/manage/bookings/${encodeURIComponent(b.id)}/decision`, { method: 'POST', body: { action, ...(amount ? { amount_cents: amount } : {}) } }); load(); }
        catch (e) { panel.append(h('div', { class: 'error', text: e.message })); }
      };
      const buttons = [];
      if (b.status === 'requested') {
        buttons.push(h('button', { class: 'btn primary', text: 'Confirm with a price', onclick: async () => {
          const cents = await askAmount({ title: 'Confirm this booking', body: `${b.guest}, ${b.party_size} people. The guest pays this price through ConnectMe.`, confirmText: 'Confirm' });
          if (cents) act('confirm', cents);
        } }), h('button', { class: 'btn danger', text: 'Decline', onclick: () => act('decline') }));
      } else if (b.status === 'confirmed') {
        buttons.push(h('button', { class: 'btn primary', text: 'Mark completed', onclick: async () => {
          if (b.amount_cents && b.payment_status !== 'unpaid') { act('complete'); return; }
          const cents = await askAmount({ title: 'Complete this booking', body: 'Confirm the final price. It sets the platform commission.', confirmText: 'Complete', current: b.amount_cents });
          if (cents) act('complete', cents);
        } }));
      }
      panel.append(h('div', { class: 'item' },
        h('div', { class: 'row spread' }, h('b', { text: b.guest }),
          h('span', { class: 'badge', text: statusLabel[b.status] || b.status })),
        h('small', { text: `${when(b.requested_for)} · ${b.party_size} ${b.party_size === 1 ? 'person' : 'people'}${b.amount_cents ? ` · ${money(b.amount_cents)}` : ''}${payLabel[b.payment_status] ? ` · ${payLabel[b.payment_status]}` : ''}` }),
        b.note ? h('div', { class: 'evidence', text: b.note }) : null,
        h('div', { class: 'row' }, buttons)));
    }
  }
  load();
}

function renderEvents(root, partner) {
  const panel = h('div', { class: 'panel' });
  root.append(panel);
  async function load() {
    clear(panel);
    const title = h('input', { type: 'text', maxlength: 80, placeholder: 'Title' });
    const about = h('textarea', { maxlength: 500, placeholder: 'What should guests know?' });
    const kind = h('select', {}, [['hangout', 'Hangout'], ['concert', 'Concert'], ['mixer', 'Mixer (hotel guests only)']].map(([v, l]) => h('option', { value: v, text: l })));
    const starts = h('input', { type: 'datetime-local' });
    const ends = h('input', { type: 'datetime-local' });
    const cap = h('input', { type: 'number', min: '1', max: '5000', placeholder: 'Capacity (optional)' });
    const price = h('input', { type: 'number', min: '0', max: '5000', placeholder: 'Ticket price in coins (0 = free)', value: '0' });
    const err = h('div', { class: 'error' });
    const create = h('button', { class: 'btn primary', text: 'Schedule event', onclick: async () => {
      err.textContent = '';
      try {
        await api(`/partners/manage/partners/${encodeURIComponent(partner.id)}/events`, { method: 'POST', body: {
          kind: kind.value, title: title.value.trim(), description: about.value.trim(),
          starts_at: new Date(starts.value).toISOString(), ends_at: new Date(ends.value).toISOString(),
          capacity: cap.value ? parseInt(cap.value, 10) : null, price_coins: parseInt(price.value || '0', 10) } });
        load();
      } catch (e) { err.textContent = e.message === 'Invalid time value' ? 'Choose a start and end time.' : e.message; }
    } });
    panel.append(h('h2', { text: 'Events' }),
      h('div', { class: 'form' }, kind, title, about, h('label', { text: 'Starts' }), starts, h('label', { text: 'Ends' }), ends, cap, price, err, create),
      h('hr'));
    let events;
    try { ({ events } = await api(`/partners/manage/partners/${encodeURIComponent(partner.id)}/events`)); }
    catch (e) { panel.append(h('div', { class: 'error', text: e.message })); return; }
    if (!events.length) { panel.append(h('div', { class: 'empty', text: 'No events yet.' })); return; }
    for (const ev of events) {
      const cancel = ev.status === 'scheduled' && new Date(ev.ends_at) > new Date()
        ? h('button', { class: 'btn danger', text: 'Cancel and refund', onclick: async () => {
            try { await api(`/partners/manage/events/${encodeURIComponent(ev.id)}`, { method: 'DELETE' }); load(); }
            catch (e) { panel.append(h('div', { class: 'error', text: e.message })); }
          } }) : null;
      panel.append(h('div', { class: 'item' },
        h('div', { class: 'row spread' }, h('b', { text: ev.title }), h('span', { class: 'badge', text: ev.status })),
        h('small', { text: `${when(ev.starts_at)} · ${ev.going}${ev.capacity ? ` of ${ev.capacity}` : ''} going · ${ev.price_coins ? `${ev.price_coins} coins` : 'free'} · ${ev.coins_earned} coins taken` }),
        h('div', { class: 'row' }, cancel)));
    }
  }
  load();
}

function renderReport(root, partner) {
  const panel = h('div', { class: 'panel' });
  root.append(panel);
  (async () => {
    let r;
    try { r = await api(`/partners/manage/partners/${encodeURIComponent(partner.id)}/report`); }
    catch (e) { panel.append(h('div', { class: 'error', text: e.message })); return; }
    const cell = (v, label) => h('div', { class: 'stat' }, h('b', { text: String(v) }), h('span', { text: label }));
    panel.append(h('h2', { text: 'Earnings' }),
      h('div', { class: 'stats' },
        cell(money(r.collected_cents), 'Collected through ConnectMe'),
        cell(money(r.commission_cents), `Platform commission (${(r.commission_bps / 100).toFixed(1)}%)`),
        cell(money(r.owed_to_partner_cents), 'Owed to you (completed, paid bookings)'),
        cell(r.refunds_due, 'Refunds being settled'),
        cell(r.bookings_completed, 'Completed bookings'),
        cell(r.ticket_coins, 'Ticket coins taken')),
      h('p', { class: 'note', text: 'Payouts are settled by the ConnectMe team outside this portal.' }));
  })();
}

// ---- start -------------------------------------------------------------
(async () => {
  if (await refreshSession()) {
    try { await loadPartners(); } catch { /* fall through to login */ }
  }
  render();
})();
