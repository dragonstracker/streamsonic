(() => {
  'use strict';

  const API = '/streamer/api/v3';
  const AUTH_KEY = 'MediaServerAuth';

  const I = {
    media: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="2.5" y="4.5" width="19" height="15" rx="2"/><path d="M10 9.5l5 2.5-5 2.5z" fill="currentColor" stroke="none"/></svg>',
    pulse: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2 12h4l2.5-6 4 12 2.5-6H22"/></svg>',
    config: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/></svg>',
    cluster: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="4.5" r="2.2"/><circle cx="5" cy="18" r="2.2"/><circle cx="19" cy="18" r="2.2"/><path d="M10.6 6.5L6.4 16M13.4 6.5l4.2 9.5M7.2 18h9.6"/></svg>',
    iptv: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3" y="6" width="18" height="12" rx="1.5"/><path d="M8 21h8"/></svg>',
    camera: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M3 8.5h11v7H3z"/><path d="M14 11l5-2.5v7L14 13"/><circle cx="7" cy="12" r="1.4"/></svg>',
    support: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="3.2"/><path d="M6.4 6.4l3.4 3.4M14.2 14.2l3.4 3.4M17.6 6.4l-3.4 3.4M9.8 14.2l-3.4 3.4"/></svg>',
    logout: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M14 4.5H6.5v15H14"/><path d="M11 12h9M17 9l3 3-3 3"/></svg>',
  };

  const NAV = [
    ['media', '#/streams', 'Media', I.media],
    ['pulse', '#/pulse', 'Pulse', I.pulse],
    ['config', '#/config', 'Config', I.config],
    ['cluster', '#/cluster', 'Cluster', I.cluster],
    ['iptv', '#/iptv/users', 'IPTV', I.iptv],
    ['watcher', '#/watcher', 'IP Cameras', I.camera],
    ['support', '#/support', 'Support', I.support],
  ];

  const MEDIA_TABS = [
    ['#/streams', 'Streams'],
    ['#/templates', 'Templates'],
    ['#/multiplexers', 'Multiplexers'],
    ['#/sources', 'Sources'],
    ['#/vods', 'VODs'],
    ['#/dvb-cards', 'DVB cards'],
  ];

  const STREAM_TABS = ['Overview', 'Input', 'Transcoder', 'DVR', 'Output', 'EPG', 'Auth', 'Play sessions'];

  const S = {
    gen: 0,
    timer: null,
    route: '',
    name: '',
    tab: 'Overview',
    sort: 'Stream',
    player: null,
    version: '',
    settings: null,
  };

  const $ = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  function esc(v) {
    return String(v == null ? '' : v).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  function bytes(v) {
    let n = Number(v || 0);
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i > 0 && n < 100 ? n.toFixed(1) : Math.round(n)) + ' ' + u[i];
  }

  function bits(v) {
    const n = Number(v || 0);
    if (n <= 0) return '0 kbit/s';
    if (n >= 1e6) return (n / 1e6).toFixed(2) + ' Mbit/s';
    return Math.round(n / 1e3) + ' kbit/s';
  }

  function mbps(v) {
    return (Number(v || 0) / 1e6).toFixed(3) + ' MBPS';
  }

  function uptime(sec) {
    const s = Math.max(0, Math.floor(Number(sec || 0)));
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    if (d) return d + 'd ' + h + 'h';
    if (h) return String(h).padStart(2, '0') + ':' + String(m).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0');
    return String(m).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0');
  }

  function since(iso) {
    if (!iso) return '';
    const t = Date.parse(iso);
    if (!t) return '';
    return uptime((Date.now() - t) / 1000);
  }

  function toast(msg, err) {
    const t = document.createElement('div');
    t.className = 'toast' + (err ? ' err' : '');
    t.textContent = msg;
    document.body.appendChild(t);
    setTimeout(() => t.remove(), 4200);
  }

  function credential() {
    try {
      const raw = localStorage.getItem(AUTH_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch {
      return null;
    }
  }

  function authHeader() {
    const c = credential();
    if (!c || !c.login) return {};
    return { Authorization: 'Bearer ' + btoa(c.login + ':' + c.password) };
  }

  async function api(path, opts) {
    const o = Object.assign({ credentials: 'same-origin' }, opts || {});
    o.headers = Object.assign({}, authHeader(), o.headers || {});
    const res = await fetch(API + path, o);
    if (res.status === 401) {
      logout();
      throw new Error('unauthorized');
    }
    const text = await res.text();
    let body = null;
    try { body = text ? JSON.parse(text) : null; } catch { body = text; }
    if (!res.ok) {
      const msg = body && body.error ? body.error : res.status + ' ' + res.statusText;
      throw new Error(msg);
    }
    return body;
  }

  function logout() {
    localStorage.removeItem(AUTH_KEY);
    stopPlayer();
    clearInterval(S.timer);
    S.timer = null;
    S.gen++;
    renderLogin('');
  }

  /* ------------------------------------------------------------- chrome */

  function wordmark(color, size) {
    return '<svg width="' + (size * 3.9) + '" height="' + size + '" viewBox="0 0 156 40" fill="none">' +
      '<text x="0" y="29" font-family="FiraSans, Helvetica, Arial, sans-serif" font-size="27" font-weight="500" fill="' + color + '">streamsonic</text>' +
      '</svg>';
  }

  function chrome(title, sub, metrics) {
    const active = (key) => {
      const r = S.route;
      if (key === 'media') return /^#?\/(streams|templates|multiplexers|sources|vods|dvb-cards|create)/.test(r);
      if (key === 'config') return /^#?\/(config|auth_backends|license|ui_settings)/.test(r);
      return r.indexOf('#/' + key) === 0;
    };
    const nav = NAV.map(([key, href, label, icon]) =>
      '<a href="' + href + '" data-nav="' + key + '" class="' + (active(key) ? 'active' : '') + '">' + icon + '<span>' + label + '</span></a>'
    ).join('');

    document.getElementById('root').innerHTML =
      '<aside class="rail">' +
        '<div class="rail-logo">' + wordmark('#fff', 13) + '</div>' +
        '<hr>' +
        '<nav class="rail-nav">' + nav + '</nav>' +
        '<div class="rail-bottom">' +
          '<hr>' +
          '<a href="#" id="logout">' + I.logout + '<span>Logout</span></a>' +
          '<a href="#" class="lang">EN</a>' +
        '</div>' +
      '</aside>' +
      '<main class="shell">' +
        '<div class="page-head">' +
          '<div><h1 class="page-title">' + title + '</h1>' + (sub ? '<p class="page-sub">' + sub + '</p>' : '') + '</div>' +
          '<div class="metrics" id="metrics">' + (metrics || '') + '</div>' +
        '</div>' +
        '<div id="view"></div>' +
      '</main>';

    const lo = $('#logout');
    if (lo) lo.onclick = (e) => { e.preventDefault(); logout(); };
  }

  function metricsHTML(st) {
    return '<div class="m"><span>STREAMS:</span><b>' + st.streams + ' / ' + (st.stream_limit || 999) + '</b></div>' +
      '<div class="m"><span>FILES:</span><b>' + (st.files || 0) + '</b></div>' +
      '<div class="m"><span>CLIENTS:</span><b>' + st.clients + '</b></div>' +
      '<div class="m"><span>IN:</span><b>' + mbps(st.bytes_in_rate || 0) + '</b></div>' +
      '<div class="m"><span>OUT:</span><b>' + mbps(st.bytes_out_rate || 0) + '</b></div>' +
      '<div class="m"><span>UP:</span><b>' + uptime(st.uptime) + '</b></div>';
  }

  async function refreshMetrics() {
    const host = $('#metrics');
    if (!host) return;
    try {
      const st = await api('/server/stats');
      host.innerHTML = metricsHTML(st);
    } catch { /* transient */ }
  }

  /* -------------------------------------------------------------- router */

  function router() {
    stopPlayer();
    clearInterval(S.timer);
    S.timer = null;
    S.gen++;
    S.route = location.hash.replace(/^#/, '') || '/streams';

    if (!credential()) { renderLogin(''); return; }

    const parts = S.route.split('?')[0].split('/').filter(Boolean);
    S.name = '';
    S.tab = 'Overview';

    const go = (key) => { location.hash = key; };

    const plain = parts[0] || 'streams';

    if (plain === 'streams' && parts.length === 1) return renderStreamList();
    if (plain === 'streams' && parts.length >= 2) { S.name = decodeURIComponent(parts[1]); return renderStreamDetail(); }
    if (plain === 'create') return renderCreate();
    if (plain === 'pulse') return renderPulse();
    if (plain === 'config') return renderConfig(parts[1] || 'settings');
    if (plain === 'cluster') return renderCluster();
    if (plain === 'iptv') return renderIPTV();
    if (plain === 'watcher') return renderWatcher();
    if (plain === 'support') return renderSupport();
    if (['templates', 'multiplexers', 'sources', 'vods', 'dvb-cards'].includes(plain)) return renderMediaTab(plain);
    go('#/streams');
  }

  function mediaToolbar(active) {
    const tabs = MEDIA_TABS.map(([href, label]) =>
      '<a href="' + href + '" class="' + (href === '#/' + active ? 'active' : '') + '">' + label + '</a>'
    ).join('');
    return '<div class="toolbar">' +
      '<a class="new-btn" href="#/create?entity=stream" title="Create">&plus;</a>' +
      '<div class="tabs">' + tabs + '</div>' +
      '</div>';
  }

  /* ----------------------------------------------------------- streams */

  function streamRow(s, idx) {
    const st = s.stats || {};
    const cfg = s.config || {};
    const input = (cfg.inputs && cfg.inputs[0] && cfg.inputs[0].url) || (s.input || '');
    const started = s.status === 'online';
    const dvr = cfg.dvr || s.dvr;
    const dvrOn = !!(cfg.dvr && cfg.dvr.enabled !== false) || !!s.dvr;

    return '<div class="stream-row" data-name="' + esc(s.name) + '">' +
      '<div class="idx"><a href="#/streams/' + encodeURIComponent(s.name) + '">' + String(idx).padStart(1, '0') + '</a></div>' +
      '<div class="col">' +
        '<a class="name" href="#/streams/' + encodeURIComponent(s.name) + '">' + esc(s.name) + '</a>' +
        '<div class="line"><b>Start:</b> ' + (started ? 'Always started' : 'On Demand') + '</div>' +
      '</div>' +
      '<div class="col">' +
        '<div class="line ' + (s.status === 'online' ? 'tag-ok' : (s.last_error ? 'tag-err' : 'tag-warn')) + '">' +
          (s.status === 'online' ? 'Online' : (s.last_error ? esc(s.last_error) : 'Waiting for clients')) + '</div>' +
        '<div class="line url">' + esc(input || 'no input') + '</div>' +
      '</div>' +
      '<div class="col">' +
        '<div class="line"><b>Uptime:</b> ' + (started ? since(s.started) : '') + '</div>' +
        '<div class="line"><b>Bitrate:</b> ' + (st.bitrate ? bits(st.bitrate) : 'n/a') + '</div>' +
        '<div class="line muted">Transcoder disabled</div>' +
      '</div>' +
      '<div class="col">' +
        '<div class="line muted">' + (dvrOn ? 'Archive enabled' : 'Archive disabled') + '</div>' +
        '<div class="line muted">Push summary: running 0</div>' +
      '</div>' +
      '<div class="ctl">' +
        '<a class="more" href="#/streams/' + encodeURIComponent(s.name) + '">More</a>' +
        '<span class="switch ' + (started ? 'on' : '') + '" data-toggle="' + esc(s.name) + '" data-on="' + (started ? '1' : '0') + '"></span>' +
      '</div>' +
    '</div>';
  }

  async function renderStreamList() {
    const gen = S.gen;
    chrome('Streams', esc(S.version || ''), '');
    const v = $('#view');
    v.innerHTML = mediaToolbar('streams') +
      '<div class="sortbar"><span class="label"><span>Sort By:</span></span>' +
        ['Stream', 'Input', 'Transcode', 'DVR', 'Output'].map((c) =>
          '<span class="col' + (S.sort === c ? ' sorted' : '') + '" data-sort="' + c + '">' + c + '</span>'
        ).join('<span class="pipe">|</span>') +
      '</div>' +
      '<div class="stream-list" id="list"><div class="empty">Loading…</div></div>';

    $$('[data-sort]', v).forEach((n) => {
      n.onclick = () => { S.sort = n.dataset.sort; renderStreamList(); };
    });

    const load = async () => {
      let list;
      try { list = await api('/streams'); } catch (e) {
        if (gen === S.gen) $('#list').innerHTML = '<div class="empty">' + esc(e.message) + '</div>';
        return;
      }
      if (gen !== S.gen) return;
      const host = $('#list');
      if (!host) return;
      if (!list || !list.length) {
        host.innerHTML = '<div class="empty">No streams yet. Use + to create one.</div>';
        return;
      }
      host.innerHTML = list.map((s, i) => streamRow(s, i + 1)).join('');
      $$('[data-toggle]', host).forEach((sw) => {
        sw.onclick = async () => {
          const on = sw.dataset.on === '1';
          sw.dataset.on = on ? '0' : '1';
          sw.classList.toggle('on');
          try {
            await api('/streams/' + encodeURIComponent(sw.dataset.toggle) + (on ? '/stop' : '/start'), { method: 'PUT' });
          } catch (e) { toast(e.message, true); }
          load().catch(() => {});
        };
      });
    };

    await load();
    refreshMetrics();
    if (gen !== S.gen) return;
    S.timer = setInterval(() => {
      if (S.gen !== gen) return;
      load().catch(() => {});
      refreshMetrics();
    }, 3000);
  }

  async function renderMediaTab(kind) {
    chrome({ templates: 'Templates', multiplexers: 'Multiplexers', sources: 'Sources', vods: 'VODs', 'dvb-cards': 'DVB cards' }[kind], '', '');
    const v = $('#view');
    v.innerHTML = mediaToolbar(kind) + '<div class="panel"><div class="empty">Loading…</div></div>';

    const messages = {
      templates: ['Templates', 'Stream templates are not available on this node.', 'Define a stream with <code>template</code> directives in the config editor.'],
      multiplexers: ['Multiplexers', 'No multiplexers configured.', 'Multiplexing (mosaic) is not enabled on this build.'],
      sources: ['Sources', 'No sources configured.', 'Shared sources appear here when a config defines <code>source</code> blocks.'],
      vods: ['VODs', 'No VOD locations configured.', 'Add <code>vod</code> locations in the config editor to serve files.'],
      'dvb-cards': ['DVB cards', 'No DVB/ATSC hardware detected.', 'This build captures network and file inputs only.'],
    };
    const [title, lead, hint] = messages[kind] || ['', '', ''];

    let extra = '';
    if (kind === 'sources' || kind === 'vods') {
      try {
        const cfg = await api('/config');
        const items = Object.keys((cfg.config && cfg.config[kind]) || {});
        if (items.length) {
          extra = '<div class="code-list">' + items.map((k) => '<code>' + esc(k) + '</code>').join('') + '</div>';
        }
      } catch { /* ignore */ }
    }

    const p = $('.panel', v);
    if (p) {
      p.innerHTML = '<h2>' + title + '</h2><div class="empty">' + lead + '</div>' +
        (extra || '<div class="empty">' + hint + '</div>');
    }
  }

  /* ------------------------------------------------------------ detail */

  async function renderStreamDetail() {
    const gen = S.gen;
    const name = S.name;
    let data;
    try { data = await api('/streams/' + encodeURIComponent(name) + '/stats'); }
    catch (e) {
      chrome('<span class="crumb"><a href="#/streams">Streams</a></span><span class="sep">/</span>' + esc(name), esc(S.version || ''), '');
      $('#view').innerHTML = '<div class="panel"><div class="empty">' + esc(e.message) + '</div></div>';
      return;
    }
    if (gen !== S.gen) return;

    chrome('<span class="crumb"><a href="#/streams">Streams</a></span><span class="sep">/</span>' + esc(name), esc(S.version || ''), '');
    refreshMetrics();

    const tabs = STREAM_TABS.map((t) =>
      '<a href="#" data-tab="' + t + '" class="' + (S.tab === t ? 'active' : '') + '">' + t + '</a>').join('');

    $('#view').innerHTML =
      '<div class="toolbar"><div class="tabbar" id="stabs">' + tabs + '</div></div>' +
      '<div id="stab"></div>';

    $$('#stabs a').forEach((a) => {
      a.onclick = (e) => { e.preventDefault(); S.tab = a.dataset.tab; renderStreamDetail(); };
    });

    const body = $('#stab');
    if (S.tab === 'Overview') renderOverview(body, name, data, gen);
    else if (S.tab === 'Input') renderInputTab(body, name, data);
    else if (S.tab === 'Transcoder') renderSimpleTab(body, 'Transcoder', 'Transcoding pipeline is not enabled on this build. Streams are remuxed without re-encoding.');
    else if (S.tab === 'DVR') renderDVR(body, name, data, gen);
    else if (S.tab === 'Output') renderOutputTab(body, name);
    else if (S.tab === 'EPG') renderSimpleTab(body, 'EPG', 'No EPG provider configured for this stream.');
    else if (S.tab === 'Auth') renderSimpleTab(body, 'Auth', 'Publish and play authorization is controlled by global settings. Per-stream auth policies are not enabled on this build.');
    else if (S.tab === 'Play sessions') renderSessions(body, name, gen);
  }

  function renderOverview(host, name, data, gen) {
    const base = location.origin;
    const mi = data.media_info || {};
    const tracks = (mi.tracks || []).filter((t) => t.type === 'video' || t.type === 'audio').map((t) => {
      const kbps = t.bitrate ? ' (' + Math.round(t.bitrate / 1000) + 'kbps)' : '';
      if (t.type === 'video') {
        return '<div class="line"><b>v1</b> ' + esc(t.codec) + ' ' + (t.width || '?') + 'x' + (t.height || '?') + kbps + '</div>';
      }
      return '<div class="line"><b>a1</b> ' + esc(t.codec) + ' ' + (t.channels === 1 ? 'mono' : 'stereo') + kbps + '</div>';
    }).join('');

    host.innerHTML =
      '<div class="panel" style="display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:24px">' +
        '<div>' +
          '<h3>Description</h3>' +
          '<div class="field"><label>Name</label><input value="' + esc(name) + '" readonly></div>' +
          '<div class="field"><label>Title</label><input id="f-title" value="' + esc((data.config && data.config.title) || '') + '"></div>' +
          '<div class="field"><label>Description</label><input id="f-desc" value="' + esc(data.comment || '') + '"></div>' +
          '<div class="row">' +
            '<button class="btn" id="btn-stop">Stop</button>' +
            '<button class="btn" id="btn-start">Disable</button>' +
            '<button class="btn danger" id="btn-delete">Delete Stream</button>' +
            '<span class="grow"></span>' +
            '<button class="btn primary" id="btn-save">Save</button>' +
          '</div>' +
        '</div>' +
        '<div>' +
          '<h3>Live preview</h3>' +
          '<div class="player" id="player"><div class="player-status">waiting for clients</div></div>' +
          '<div class="code-list" style="margin-top:12px">' +
            '<code>' + base + '/stream/' + esc(name) + '/index.m3u8</code>' +
            '<code>' + base + '/stream/' + esc(name) + '/index.mpd</code>' +
          '</div>' +
        '</div>' +
      '</div>' +
      '<div class="grid2" style="margin-top:16px">' +
        '<div class="panel"><h2>Input media info</h2>' + (tracks || '<div class="empty">No media received</div>') + '</div>' +
        '<div class="panel"><h2>Output media info</h2>' + (tracks || '<div class="empty">No media received</div>') + '</div>' +
      '</div>';

    $('#btn-stop').onclick = async () => {
      try { await api('/streams/' + encodeURIComponent(name) + '/stop', { method: 'PUT' }); toast('Stream stopped'); }
      catch (e) { toast(e.message, true); }
    };
    $('#btn-start').onclick = async () => {
      try { await api('/streams/' + encodeURIComponent(name) + '/start', { method: 'PUT' }); toast('Stream started'); }
      catch (e) { toast(e.message, true); }
    };
    $('#btn-delete').onclick = async () => {
      if (!confirm('Delete stream ' + name + '?')) return;
      try {
        await api('/streams/' + encodeURIComponent(name), { method: 'DELETE' });
        location.hash = '#/streams';
      } catch (e) { toast(e.message, true); }
    };
    $('#btn-save').onclick = async () => {
      try {
        const full = Object.assign({}, data.config || {});
        full.name = name;
        full.title = $('#f-title').value;
        full.comment = $('#f-desc').value;
        await api('/streams/' + encodeURIComponent(name), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(full),
        });
        toast('Stream saved');
      } catch (e) { toast(e.message, true); }
    };

    stopPlayer();
    S.player = new Preview(name, $('#player'));
    S.player.start();
  }

  function renderInputTab(host, name, data) {
    const cfg = data.config || {};
    const inputs = cfg.inputs || [];
    host.innerHTML = '<div class="panel"><h2>Input</h2>' +
      '<div class="kv">' +
        '<span class="k">Status</span><span class="v">' + esc(data.status) + '</span>' +
        '<span class="k">Started</span><span class="v">' + esc(data.started || '') + '</span>' +
        '<span class="k">Input switches</span><span class="v">' + (data.stats.input_switches || 0) + '</span>' +
        '<span class="k">Input errors</span><span class="v">' + (data.stats.input_errors || 0) + '</span>' +
        '<span class="k">Ingested</span><span class="v">' + bytes(data.stats.bytes_in) + '</span>' +
      '</div>' +
      '<h2 style="margin-top:18px">Sources</h2>' +
      (inputs.length
        ? '<div class="code-list">' + inputs.map((i) => '<code>' + esc(i.url || '') + '</code>').join('') + '</div>'
        : '<div class="empty">No input configured</div>') +
      '</div>';
  }

  function renderOutputTab(host, name) {
    const base = location.origin;
    host.innerHTML = '<div class="panel"><h2>Delivery</h2>' +
      '<div class="code-list">' +
        ['index.m3u8', 'index.mpd', 'info.json', 'ranges.json'].map((f) =>
          '<code>' + base + '/stream/' + esc(name) + '/' + f + '</code>').join('') +
      '</div>' +
      '<h2 style="margin-top:18px">Embed HTML player on your website</h2>' +
      '<div class="field"><textarea rows="5" readonly>&lt;video src="' + base + '/stream/' + esc(name) + '/index.m3u8" controls playsinline&gt;&lt;/video&gt;</textarea></div>' +
      '</div>';
  }

  function renderSimpleTab(host, title, text) {
    host.innerHTML = '<div class="panel"><h2>' + title + '</h2><div class="empty">' + text + '</div></div>';
  }

  async function renderDVR(host, name, data, gen) {
    host.innerHTML = '<div class="panel"><h2>DVR</h2><div class="empty">Loading…</div></div>';
    let d;
    try { d = await api('/streams/' + encodeURIComponent(name) + '/dvr'); }
    catch (e) { host.innerHTML = '<div class="panel"><h2>DVR</h2><div class="empty">' + esc(e.message) + '</div></div>'; return; }
    if (gen !== S.gen) return;
    if (!d || !d.enabled) {
      host.innerHTML = '<div class="panel"><h2>DVR</h2><div class="empty">DVR is not configured for this stream.</div></div>';
      return;
    }
    const ranges = (d.ranges || []).map((r) =>
      '<div class="kv"><span class="k mono">' + esc(r.from) + '</span><span class="v mono">' + esc(r.to) + '</span></div>').join('');
    host.innerHTML = '<div class="panel"><h2>Archive</h2><div class="kv">' +
      '<span class="k">Size</span><span class="v">' + bytes(d.size) + '</span>' +
      '<span class="k">Segments</span><span class="v">' + (d.entries || 0) + '</span>' +
      '<span class="k">Expiration</span><span class="v">' + (d.expiration || 0) + 's</span>' +
      '</div><h2 style="margin-top:18px">Ranges</h2>' +
      (ranges || '<div class="empty">No recorded data yet</div>') + '</div>';
  }

  async function renderSessions(host, name, gen) {
    host.innerHTML = '<div class="panel"><h2>Play sessions</h2><div class="empty">Loading…</div></div>';
    let list;
    try { list = await api('/streams/' + encodeURIComponent(name) + '/sessions'); }
    catch (e) { host.innerHTML = '<div class="panel"><h2>Play sessions</h2><div class="empty">' + esc(e.message) + '</div></div>'; return; }
    if (gen !== S.gen) return;
    if (!list || !list.length) {
      host.innerHTML = '<div class="panel"><h2>Play sessions</h2><div class="empty">No active sessions.</div></div>';
      return;
    }
    const rows = list.map((s) =>
      '<div class="kv"><span class="k mono">' + esc(s.id || '') + '</span><span class="v">' +
      esc(s.ip || '') + ' · ' + esc(s.protocol || '') + ' · ' + bytes(s.bytes_out) + '</span></div>').join('');
    host.innerHTML = '<div class="panel"><h2>Play sessions</h2>' + rows + '</div>';
  }

  /* ------------------------------------------------------------- create */

  function renderCreate() {
    chrome('Create', '', '');
    $('#view').innerHTML = mediaToolbar('streams') +
      '<div class="panel" style="max-width:760px">' +
        '<h2>New stream</h2>' +
        '<div class="field"><label>Name</label><input id="c-name" placeholder="channel-1"></div>' +
        '<div class="field"><label>Input URL</label><input id="c-url" placeholder="tshttp://host/live.ts"></div>' +
        '<div class="field"><label>Title</label><input id="c-title" placeholder="Optional"></div>' +
        '<div class="field"><label>DVR</label><select id="c-dvr"><option value="">Disabled</option></select></div>' +
        '<div class="row"><button class="btn primary" id="c-save">Create</button></div>' +
      '</div>';

    api('/config').then((cfg) => {
      const sel = $('#c-dvr');
      const dvrs = Object.keys(cfg.config.dvrs || {});
      if (!sel || !dvrs.length) return;
      sel.innerHTML = '<option value="">Disabled</option>' + dvrs.map((d) => '<option value="' + esc(d) + '">' + esc(d) + '</option>').join('');
    }).catch(() => {});

    $('#c-save').onclick = async () => {
      const name = $('#c-name').value.trim();
      const url = $('#c-url').value.trim();
      if (!name || !url) { toast('Name and input URL are required', true); return; }
      const body = { name, inputs: [{ url }] };
      const title = $('#c-title').value.trim();
      if (title) body.title = title;
      const dvr = $('#c-dvr').value;
      if (dvr) body.dvr = { dvr };
      try {
        await api('/streams', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        toast('Stream created');
        location.hash = '#/streams/' + encodeURIComponent(name);
      } catch (e) { toast(e.message, true); }
    };
  }

  /* -------------------------------------------------------------- pulse */

  async function renderPulse() {
    const gen = S.gen;
    chrome('Pulse', '', '');
    const v = $('#view');
    v.innerHTML = '<div class="stat-grid" id="pulse-grid"><div class="stat"><div class="label">Loading</div><div class="value">–</div></div></div>' +
      '<div class="panel" style="margin-top:16px"><h2>Node health</h2><div class="kv" id="pulse-kv"></div></div>';

    const load = async () => {
      let st;
      try { st = await api('/server/stats'); } catch (e) { return; }
      if (gen !== S.gen) return;
      const grid = $('#pulse-grid');
      if (!grid) return;
      const cells = [
        ['Streams', st.streams], ['Clients', st.clients], ['Sessions', st.sessions],
        ['Segments', st.segments], ['Ingested', bytes(st.bytes_in)], ['Delivered', bytes(st.bytes_out)],
        ['Goroutines', st.goroutines], ['Heap', bytes(st.heap_alloc)], ['Uptime', uptime(st.uptime)],
      ];
      grid.innerHTML = cells.map(([k, val]) =>
        '<div class="stat"><div class="label">' + k + '</div><div class="value">' + val + '</div></div>').join('');
      const kv = $('#pulse-kv');
      if (kv) {
        kv.innerHTML =
          '<span class="k">Version</span><span class="v">' + esc(st.version) + '</span>' +
          '<span class="k">Go runtime</span><span class="v">' + esc(st.go_version) + '</span>' +
          '<span class="k">CPUs</span><span class="v">' + st.num_cpu + '</span>' +
          '<span class="k">Config revision</span><span class="v">' + st.config_rev + '</span>' +
          '<span class="k">Resident sys</span><span class="v">' + bytes(st.sys) + '</span>';
      }
      refreshMetrics();
    };

    await load();
    if (gen !== S.gen) return;
    S.timer = setInterval(load, 5000);
  }

  /* ------------------------------------------------------------- config */

  const CONFIG_TABS = [
    ['settings', 'Settings'],
    ['editor', 'Config editor'],
    ['dvrs', 'DVR'],
    ['auth', 'Auth'],
    ['backends', 'Auth backends'],
    ['events', 'Events'],
  ];

  async function renderConfig(sub) {
    const gen = S.gen;
    chrome('Config', '', '');
    const tabs = CONFIG_TABS.map(([k, label]) =>
      '<a href="#/config' + (k === 'settings' ? '' : '/' + k) + '" class="' + (sub === k ? 'active' : '') + '">' + label + '</a>').join('');
    $('#view').innerHTML = '<div class="toolbar"><div class="tabbar">' + tabs + '</div></div><div id="cfg-view"></div>';
    const host = $('#cfg-view');

    if (sub === 'editor') return configEditor(host, gen);

    let cfg;
    try { cfg = await api('/config'); } catch (e) { host.innerHTML = '<div class="panel"><div class="empty">' + esc(e.message) + '</div></div>'; return; }
    if (gen !== S.gen) return;
    const c = cfg.config || {};

    if (sub === 'dvrs') {
      const list = Array.isArray(c.dvrs) ? c.dvrs : [];
      host.innerHTML = '<div class="panel"><h2>DVR storage</h2>' +
        (list.length
          ? '<div class="kv">' + list.map((d) =>
              '<span class="k">' + esc(d.name) + '</span><span class="v">' + esc(d.root || '') +
              (d.expiration ? ' · ' + d.expiration + 's' : '') + '</span>').join('') + '</div>'
          : '<div class="empty">No DVR configured.</div>') + '</div>';
      return;
    }

    if (sub === 'auth') {
      host.innerHTML = '<div class="panel"><h2>Editor authorization</h2><div class="kv">' +
        '<span class="k">Login</span><span class="v">' + esc(c.edit_auth || 'disabled') + '</span>' +
        '<span class="k">Protection</span><span class="v">' + (c.edit_auth ? 'HTTP Basic + Bearer' : 'none') + '</span>' +
        '</div></div>';
      return;
    }

    if (sub === 'backends') {
      host.innerHTML = '<div class="panel"><h2>Auth backends</h2><div class="empty">No external authentication backends configured.</div></div>';
      return;
    }

    if (sub === 'events') {
      const sinks = Array.isArray(c.event_sinks) ? c.event_sinks : [];
      host.innerHTML = '<div class="panel"><h2>Event sinks</h2>' +
        (sinks.length
          ? '<div class="code-list">' + sinks.map((s) => '<code>' + esc(typeof s === 'string' ? s : JSON.stringify(s)) + '</code>').join('') + '</div>'
          : '<div class="empty">No event sinks configured (' + (c.event_sinks || 0) + ').</div>') + '</div>';
      return;
    }

    host.innerHTML = '<div class="panel"><h2>Summary</h2><div class="kv">' +
      '<span class="k">Streams</span><span class="v">' + ((c.streams || []).length) + '</span>' +
      '<span class="k">DVR storage</span><span class="v">' + ((c.dvrs || []).length) + '</span>' +
      '<span class="k">Listener</span><span class="v">' + ((c.http && c.http.port) || 80) + '</span>' +
      '<span class="k">Config version</span><span class="v">' + esc((cfg.config_version || []).join(':')) + '</span>' +
      '</div><h2 style="margin-top:18px">Listener</h2><div class="code-list">' +
      '<code>http://' + location.host + '/</code>' +
      '</div></div>';
  }

  async function configEditor(host, gen) {
    let text = '';
    try { text = await (await fetch(API + '/config/text', { headers: authHeader() })).text(); }
    catch (e) { toast(e.message, true); }
    if (gen !== S.gen) return;
    host.innerHTML = '<div class="panel">' +
      '<h2>Configuration</h2>' +
      '<div class="field"><textarea id="cfg" rows="26" spellcheck="false"></textarea></div>' +
      '<div class="row"><button class="btn primary" id="cfg-save">Save configuration</button>' +
      '<span class="empty" id="cfg-status" style="padding:0"></span></div></div>';
    $('#cfg').value = text;
    $('#cfg-save').onclick = async () => {
      try {
        const res = await fetch(API + '/config', {
          method: 'PUT',
          headers: Object.assign({ 'Content-Type': 'text/plain' }, authHeader()),
          body: $('#cfg').value,
        });
        if (!res.ok) {
          const b = await res.json().catch(() => ({}));
          throw new Error(b.error || res.statusText);
        }
        $('#cfg-status').textContent = 'Saved at ' + new Date().toLocaleTimeString();
        toast('Configuration saved');
      } catch (e) { toast(e.message, true); }
    };
  }

  /* ------------------------------------------------------- static pages */

  async function renderCluster() {
    chrome('Cluster', '', '');
    let st = {};
    try { st = await api('/server/stats'); } catch { /* ignore */ }
    $('#view').innerHTML = '<div class="panel"><h2>Cluster</h2>' +
      '<div class="empty">This node runs standalone. Cluster peering is not enabled on this build.</div>' +
      '<div class="kv"><span class="k">Node</span><span class="v">' + esc(location.hostname) + '</span>' +
      '<span class="k">Role</span><span class="v">standalone</span>' +
      '<span class="k">Streams</span><span class="v">' + (st.streams || 0) + '</span></div></div>';
  }

  async function renderIPTV() {
    chrome('IPTV', '', '');
    $('#view').innerHTML = '<div class="toolbar"><div class="tabbar">' +
      ['Users', 'Packages', 'Subscribers'].map((t, i) =>
        '<a href="#" class="' + (i === 0 ? 'active' : '') + '">' + t + '</a>').join('') +
      '</div></div><div class="panel"><h2>Users</h2><div class="empty">IPTV subscriber management is not enabled on this build.</div></div>';
  }

  async function renderWatcher() {
    chrome('IP Cameras', '', '');
    $('#view').innerHTML = '<div class="panel"><h2>Cameras</h2>' +
      '<div class="empty">ONVIF discovery and camera management are not enabled on this build.</div>' +
      '<div class="empty">Add a camera as a stream with an <code>rtsp://</code> or <code>tshttp://</code> input.</div></div>';
  }

  async function renderSupport() {
    chrome('Support', '', '');
    let st = {};
    try { st = await api('/server/stats'); } catch { /* ignore */ }
    $('#view').innerHTML = '<div class="panel"><h2>Diagnostics</h2><div class="kv">' +
      '<span class="k">Version</span><span class="v">' + esc(st.version) + '</span>' +
      '<span class="k">Go runtime</span><span class="v">' + esc(st.go_version) + '</span>' +
      '<span class="k">Uptime</span><span class="v">' + uptime(st.uptime) + '</span>' +
      '<span class="k">Goroutines</span><span class="v">' + (st.goroutines || 0) + '</span>' +
      '<span class="k">Heap</span><span class="v">' + bytes(st.heap_alloc) + '</span>' +
      '</div><h2 style="margin-top:18px">Endpoints</h2><div class="code-list">' +
      '<code>' + location.origin + '/streamer/api/v3/version</code>' +
      '<code>' + location.origin + '/streamer/api/v3/server/stats</code>' +
      '<code>' + location.origin + '/streamer/api/v3/monitoring/liveness</code>' +
      '</div></div>';
  }

  /* -------------------------------------------------------------- login */

  function renderLogin(err) {
    document.getElementById('root').innerHTML =
      '<div class="login-wrap"><form class="login-card" id="login">' +
        '<div class="login-logo">' + wordmark('#002dab', 22) + '</div>' +
        '<div class="field"><label>Login</label><input id="l-login" autocomplete="username" autofocus></div>' +
        '<div class="field"><label>Password</label><input id="l-pass" type="password" autocomplete="current-password"></div>' +
        '<button class="btn primary" type="submit">Sign</button>' +
        (err ? '<div class="login-err">' + esc(err) + '</div>' : '') +
      '</form></div>';

    $('#login').onsubmit = async (e) => {
      e.preventDefault();
      const login = $('#l-login').value.trim();
      const password = $('#l-pass').value;
      localStorage.setItem(AUTH_KEY, JSON.stringify({ login, password }));
      try {
        await api('/streams');
        if (!location.hash) location.hash = '#/streams';
        router();
      } catch (ex) {
        localStorage.removeItem(AUTH_KEY);
        renderLogin(ex.message === 'unauthorized' ? 'Wrong login or password' : ex.message);
      }
    };
  }

  /* ------------------------------------------------------------- player */

  function stopPlayer() {
    if (S.player) { S.player.destroy(); S.player = null; }
  }

  class Preview {
    constructor(name, host) {
      this.name = name;
      this.host = host;
      this.last = 0;
      this.queue = [];
      this.busy = false;
      this.dead = false;
    }

    async start() {
      this.video = document.createElement('video');
      this.video.controls = true;
      this.video.muted = true;
      this.video.autoplay = true;
      this.video.playsInline = true;
      this.host.innerHTML = '';
      this.host.appendChild(this.video);
      this.status = document.createElement('div');
      this.status.className = 'player-status';
      this.status.textContent = 'waiting for clients';
      this.host.appendChild(this.status);

      let manifest;
      try {
        manifest = await (await fetch('/stream/' + encodeURIComponent(this.name) + '/preview.json')).json();
      } catch {
        this.status.textContent = 'stream is not ready';
        return;
      }
      if (this.dead) return;
      if (!manifest || !manifest.codecs) {
        this.status.textContent = 'no playable track yet';
        setTimeout(() => { if (!this.dead) this.start(); }, 3000);
        return;
      }
      this.codecs = manifest.codecs;
      this.ms = new MediaSource();
      this.video.src = URL.createObjectURL(this.ms);
      this.ms.addEventListener('sourceopen', () => this.open());
    }

    async open() {
      if (this.dead) return;
      const mime = 'video/mp4; codecs="' + this.codecs + '"';
      if (!MediaSource.isTypeSupported(mime)) {
        this.status.textContent = 'codec not supported: ' + this.codecs;
        return;
      }
      this.sb = this.ms.addSourceBuffer(mime);
      this.sb.mode = 'segments';
      this.sb.addEventListener('updateend', () => this.pump());
      try {
        const init = await (await fetch('/stream/' + encodeURIComponent(this.name) + '/preview-init.mp4')).arrayBuffer();
        this.sb.appendBuffer(init);
        this.status.textContent = 'buffering…';
      } catch {
        this.status.textContent = 'failed to load init segment';
        return;
      }
      this.timer = setInterval(() => this.poll(), 1000);
    }

    async poll() {
      if (this.dead) return;
      try {
        const m = await (await fetch('/stream/' + encodeURIComponent(this.name) + '/preview.json')).json();
        for (const s of m.segments || []) {
          if (s.n > this.last && !this.queue.includes(s.n)) this.queue.push(s.n);
        }
        this.pump();
      } catch { /* transient */ }
    }

    async pump() {
      if (this.dead || this.busy || !this.sb || this.sb.updating) return;
      if (!this.queue.length) { this.trim(); return; }
      const n = this.queue.shift();
      this.busy = true;
      try {
        const res = await fetch('/stream/' + encodeURIComponent(this.name) + '/preview-' + n + '.m4s');
        if (!res.ok) throw new Error('segment ' + n + ' unavailable');
        const buf = await res.arrayBuffer();
        this.sb.appendBuffer(buf);
        this.last = n;
        this.status.textContent = 'live · segment ' + n;
        if (this.video.buffered.length) {
          const end = this.video.buffered.end(this.video.buffered.length - 1);
          if (this.video.currentTime < end - 2) this.video.currentTime = end - 1.5;
        }
        if (this.video.paused) this.video.play().catch(() => {});
      } catch (e) {
        this.status.textContent = e.message;
      }
      this.busy = false;
      this.pump();
    }

    trim() {
      if (!this.sb || this.sb.updating || !this.video.buffered.length) return;
      const b = this.video.buffered;
      const end = b.end(b.length - 1);
      if (end - b.start(0) > 40) this.sb.remove(0, end - 20);
    }

    destroy() {
      this.dead = true;
      clearInterval(this.timer);
      if (this.video) { this.video.pause(); this.video.src = ''; }
    }
  }

  /* -------------------------------------------------------------- boot */

  window.addEventListener('hashchange', router);
  window.addEventListener('DOMContentLoaded', () => {
    api('/version').then((v) => { S.version = v.version + ' · ' + v.edition; }).catch(() => {});
    router();
  });
})();
