// Student pages: session list and the exam room (statement, editor, submit,
// live results, announcements, countdown, finish).
import { get, post, upload, request } from './api.js';
import * as live from './live.js';
import {
  html, mount, $, busy, toast, modal, showError, errText, fmtTime, fmtClock, fmtDuration, stateBadge, resultBadge,
} from './ui.js';

export const studentNav = [['/sessions', 'Phiên thi']];

export const studentRoutes = [
  [/^\/sessions$/, sessionsPage],
  [/^\/sessions\/(\d+)(?:\/(\d+))?$/, roomPage],
];

const MAX_SOURCE = 64 * 1024;

// Server clock: every response with server_time updates the offset.
let offset = 0;
const serverNow = () => Date.now() + offset;
function syncClock(serverTime) { if (serverTime) offset = Date.parse(serverTime) - Date.now(); }

// ---------------------------------------------------------------- list

async function sessionsPage(root, _, ctx) {
  const load = async () => {
    const list = await get('/me/sessions', { pageSize: 200 });
    syncClock(list.server_time);
    // Running first, then upcoming (soonest first), then ended (latest first).
    const ORDER = { running: 0, upcoming: 1, ended: 2 };
    list.data.sort((a, b) => ORDER[a.state] - ORDER[b.state] ||
      (a.state === 'ended' ? Date.parse(b.starts_at) - Date.parse(a.starts_at) : Date.parse(a.starts_at) - Date.parse(b.starts_at)));
    mount(root, html`
      <h1>Phiên thi</h1>
      <table><tr><th>Đề</th><th>Phiên</th><th>Thời gian</th><th>Trạng thái</th><th></th><th></th></tr>
      ${list.data.map((s) => html`<tr>
        <td>${s.exam_title}</td><td>${s.name}</td>
        <td class="small">${fmtTime(s.starts_at)} → ${fmtTime(s.ends_at)}</td>
        <td>${stateBadge(s.state)} ${s.participation ? html`<span class="badge ${s.participation.status === 'finished' ? '' : 'info'}">${s.participation.status === 'finished' ? 'Bạn đã kết thúc' : 'Bạn đang làm'}</span>` : ''}</td>
        <td class="small" data-cd="${s.state === 'upcoming' ? s.starts_at : s.state === 'running' ? s.ends_at : ''}" data-kind="${s.state}"></td>
        <td>${actionButton(s)}</td></tr>`)}
      ${list.data.length ? '' : html`<tr><td colspan="6" class="muted">Chưa có phiên thi nào</td></tr>`}
      </table>`);
    tick();
  };
  const tick = () => {
    for (const td of root.querySelectorAll('[data-cd]')) {
      if (!td.dataset.cd) continue;
      const left = Date.parse(td.dataset.cd) - serverNow();
      td.textContent = left <= 0 ? '' : (td.dataset.kind === 'upcoming' ? 'Bắt đầu sau ' : 'Còn ') + fmtDuration(left);
      if (left <= 0 && td.dataset.reloaded !== '1') { td.dataset.reloaded = '1'; load().catch(() => {}); }
    }
  };
  root.onclick = (e) => {
    const b = e.target.closest('button[data-join]');
    if (!b) return;
    busy(b, async () => { await post(`/sessions/${b.dataset.join}/join`); location.hash = `#/sessions/${b.dataset.join}`; });
  };
  ctx.every(1000, tick);
  ctx.every(30_000, () => load().catch(() => {}));
  await load();
}

function actionButton(s) {
  if (s.participation) return html`<a class="btn" href="#/sessions/${s.id}">${s.state === 'running' && s.participation.status !== 'finished' ? 'Vào làm bài' : 'Xem'}</a>`;
  if (s.state === 'running') return html`<button class="primary" data-join="${s.id}">Tham gia</button>`;
  if (s.state === 'upcoming') return html`<span class="muted small">Chưa mở</span>`;
  return '';
}

// ---------------------------------------------------------------- exam room

async function roomPage(root, [sid, pidParam], ctx) {
  let info = await get(`/sessions/${sid}`);
  syncClock(info.server_time);
  let s = info.session;
  let part = info.participation;

  // Not joined yet: join automatically while the session is running.
  if (!part && s.state === 'running') {
    try { part = await post(`/sessions/${sid}/join`); info = await get(`/sessions/${sid}`); syncClock(info.server_time); s = info.session; }
    catch (e) { showError(e); }
  }
  live.subscribe(Number(sid));

  const problems = info.problems || [];
  const pid = pidParam || (problems[0] && String(problems[0].id));
  const canSubmit = () => part && part.status !== 'finished' && serverNow() >= Date.parse(s.starts_at) && serverNow() < Date.parse(s.ends_at);

  mount(root, html`
    <p><a href="#/sessions">← Phiên thi</a></p>
    <div class="panel row">
      <div><div class="muted small">${s.exam_title}</div><h1 style="margin:0">${s.name}</h1></div>
      ${stateBadge(s.state)}
      ${part ? html`<span class="badge ${part.status === 'finished' ? '' : 'info'}">${part.status === 'finished' ? 'Bạn đã kết thúc bài làm' : 'Đang làm bài'}</span>` : ''}
      <span class="spacer"></span>
      <div style="text-align:right"><div class="muted small" id="cd-label"></div><div class="countdown" id="cd"></div></div>
      ${part && part.status !== 'finished' && s.state === 'running' ? html`<button class="danger" id="finish">Kết thúc làm bài</button>` : ''}
    </div>
    <div id="anns"></div>
    ${!part ? html`<div class="panel">${s.state === 'upcoming' ? 'Phiên chưa bắt đầu — trang sẽ tự mở khi đến giờ.' : 'Bạn không tham gia phiên này.'}</div>` : ''}
    ${part && problems.length ? html`
      <div class="problem-tabs" id="ptabs">${problems.map((p) =>
        html`<button data-p="${p.id}" class="${String(p.id) === pid ? 'active' : ''}"><b>${p.code}</b> ${p.title} <span data-state="${p.id}"></span></button>`)}</div>
      <div class="grid2">
        <div id="statement"><p class="muted">Đang tải đề…</p></div>
        <div>
          <div class="panel" id="submit-box"></div>
          <h3>Bài đã nộp cho bài này</h3>
          <div id="subs"></div>
        </div>
      </div>` : ''}
    ${part && !problems.length && info.problems_hidden ? html`<div class="panel muted">Đề sẽ hiện khi phiên bắt đầu.</div>` : ''}`);

  // Countdown; reload the room when the session starts or ends.
  const startMs = Date.parse(s.starts_at), endMs = Date.parse(s.ends_at);
  const tick = () => {
    const now = serverNow();
    const lbl = $('#cd-label', root), cd = $('#cd', root);
    if (!cd) return;
    if (now < startMs) { lbl.textContent = 'Bắt đầu sau'; cd.textContent = fmtDuration(startMs - now); cd.className = 'countdown'; }
    else if (now < endMs) { lbl.textContent = 'Thời gian còn lại'; cd.textContent = fmtDuration(endMs - now); cd.className = 'countdown' + (endMs - now < 5 * 60_000 ? ' low' : ''); }
    else { lbl.textContent = ''; cd.textContent = 'Hết giờ'; cd.className = 'countdown low'; }
    const state = now < startMs ? 'upcoming' : now < endMs ? 'running' : 'ended';
    if (state !== s.state) { s = { ...s, state }; reloadPage(); }
  };
  ctx.every(1000, tick);
  tick();

  const finishBtn = $('#finish', root);
  if (finishBtn) finishBtn.onclick = () => {
    if (!confirm('Kết thúc làm bài? Sau khi kết thúc bạn KHÔNG nộp thêm được nữa.')) return;
    busy(finishBtn, async () => { await post(`/sessions/${sid}/finish`); toast('Đã kết thúc làm bài', 'ok'); reloadPage(); });
  };

  // Announcements (REST on load / resync, WS live).
  const loadAnns = async (highlight) => {
    const r = await get(`/sessions/${sid}/announcements`);
    mount($('#anns', root), r.data.map((a, i) =>
      html`<div class="announce ${highlight && i === 0 ? 'new' : ''}">📢 <span class="muted small">${fmtClock(a.created_at)}</span> ${a.text}</div>`));
  };
  ctx.on('announcement', (a) => { if (String(a.session_id) === String(sid)) { toast('📢 ' + a.text); loadAnns(true).catch(() => {}); } });
  loadAnns(false).catch(() => {});

  if (!part || !problems.length) {
    ctx.on('resync', () => loadAnns(false).catch(() => {}));
    return;
  }

  $('#ptabs', root).onclick = (e) => {
    const b = e.target.closest('button[data-p]');
    if (b) location.hash = `#/sessions/${sid}/${b.dataset.p}`;
  };

  // Problem states from /summary/me.
  const loadStates = async () => {
    const sum = await get(`/sessions/${sid}/summary/me`);
    const row = sum.rows && sum.rows[0];
    if (!row) return;
    for (const c of row.cells) {
      const span = root.querySelector(`[data-state="${c.problem_id}"]`);
      if (!span) continue;
      span.className = 'badge ' + ({ ac: 'ok', attempted: 'bad', pending: 'wait' }[c.state] || '');
      span.textContent = { ac: 'AC', attempted: '✗', pending: '…', none: '' }[c.state];
      if (c.state === 'none') span.className = '';
    }
  };

  // Statement.
  get(`/sessions/${sid}/problems/${pid}`).then((p) => {
    mount($('#statement', root), html`<div class="panel">
      <h2 style="margin-top:0">${p.code}. ${p.title}</h2>
      <p class="small muted">Giới hạn: ${p.time_limit_ms} ms · ${p.memory_limit_mb} MB · C++17 (g++ -O2)</p>
      <div class="statement">${p.statement}</div>
      <p class="small muted">Đọc từ bàn phím (stdin), in ra màn hình (stdout).</p></div>`);
  }).catch((e) => mount($('#statement', root), html`<div class="panel errors">${errText(e)}</div>`));

  // Editor + submit.
  const draftKey = `lanjudge.draft.${sid}.${pid}`;
  let draft = '';
  try { draft = localStorage.getItem(draftKey) || ''; } catch {}
  mount($('#submit-box', root), html`
    <h3 style="margin-top:0">Nộp bài</h3>
    <div class="editor"><div class="gutter" id="gutter">1</div>
      <textarea class="code" id="code" spellcheck="false" placeholder="Dán hoặc gõ mã C++ vào đây…">${draft}</textarea></div>
    <div class="row" style="margin-top:8px">
      <span class="small muted" id="stats"></span><span class="spacer"></span>
      <label class="small" style="flex-direction:row;align-items:center;gap:6px">hoặc chọn file
        <input type="file" id="file" accept=".cpp"></label>
      <button class="primary" id="send" ${canSubmit() ? '' : 'disabled'}>Nộp bài</button>
    </div>
    <div class="small muted">${canSubmit() ? 'Chỉ nhận file .cpp ≤ 64 KB. Bản nháp được lưu tự động trên máy này.' : 'Không thể nộp: phiên chưa mở, đã hết giờ, hoặc bạn đã kết thúc.'}</div>`);
  const ta = $('#code', root), gutter = $('#gutter', root), stats = $('#stats', root);
  const refreshEditor = () => {
    const lines = ta.value.split('\n').length;
    gutter.textContent = Array.from({ length: lines }, (_, i) => i + 1).join('\n');
    const bytes = new Blob([ta.value]).size;
    stats.textContent = `${lines} dòng · ${(bytes / 1024).toFixed(1)} KB`;
    stats.style.color = bytes > MAX_SOURCE ? 'var(--bad)' : '';
  };
  ta.addEventListener('input', () => { refreshEditor(); try { localStorage.setItem(draftKey, ta.value); } catch {} });
  ta.addEventListener('scroll', () => { gutter.scrollTop = ta.scrollTop; });
  ta.addEventListener('keydown', (e) => {
    if (e.key !== 'Tab') return;
    e.preventDefault();
    const { selectionStart: a, selectionEnd: b, value: v } = ta;
    if (!e.shiftKey && a === b) { ta.setRangeText('    ', a, b, 'end'); }
    else {
      // Indent / dedent every selected line.
      const ls = v.lastIndexOf('\n', a - 1) + 1;
      const block = v.slice(ls, b);
      const out = e.shiftKey ? block.replace(/^ {1,4}/gm, '') : block.replace(/^/gm, '    ');
      ta.setRangeText(out, ls, b, 'select');
    }
    ta.dispatchEvent(new Event('input'));
  });
  refreshEditor();

  const fileInput = $('#file', root);
  fileInput.onchange = async () => {
    const f = fileInput.files[0];
    if (!f) return;
    if (!/\.cpp$/i.test(f.name)) { toast('Chỉ nhận file .cpp', 'error'); fileInput.value = ''; return; }
    if (f.size > MAX_SOURCE) { toast('File lớn hơn 64 KB', 'error'); fileInput.value = ''; return; }
    ta.value = await f.text();
    ta.dispatchEvent(new Event('input'));
  };

  $('#send', root).onclick = (e) => {
    const src = ta.value;
    const bytes = new Blob([src]).size;
    if (!src.trim()) { toast('Chưa có mã nguồn', 'error'); return; }
    if (bytes > MAX_SOURCE) { toast('Mã nguồn lớn hơn 64 KB', 'error'); return; }
    const fd = new FormData();
    fd.append('file', new File([src], 'solution.cpp', { type: 'text/x-c++src' }));
    busy(e.target, async () => {
      const sub = await upload(`/sessions/${sid}/problems/${pid}/submissions`, fd);
      toast(`Đã nhận bài #${sub.id} — đang chờ chấm`, 'ok');
      fileInput.value = '';
      upsert(sub);
      loadStates().catch(() => {});
    });
  };

  // Submissions of this problem, updated live.
  const subsBox = $('#subs', root);
  const subs = new Map();
  const drawSubs = () => {
    const list = [...subs.values()].sort((a, b) => b.id - a.id);
    mount(subsBox, html`<table><tr><th>#</th><th>Lúc nộp</th><th>Kết quả</th><th></th></tr>
      ${list.map((x) => html`<tr><td>${x.id}</td><td class="small">${fmtClock(x.received_at)}</td><td>${resultBadge(x.result)}</td>
        <td><button data-view="${x.id}">Xem</button></td></tr>`)}
      ${list.length ? '' : html`<tr><td colspan="4" class="muted">Chưa nộp lần nào</td></tr>`}</table>`);
  };
  // The WS event for a fast judge can arrive before the 202 response of the
  // upload, so never replace a later state of the same run with an earlier one.
  const RANK = { queued: 0, judging: 1, completed: 2, system_error: 2 };
  const upsert = (x) => {
    const old = subs.get(x.id);
    const a = old && old.result, b = x.result;
    if (a && b && (b.run_id < a.run_id || (b.run_id === a.run_id && RANK[b.status] < RANK[a.status]))) return;
    subs.set(x.id, x);
    drawSubs();
  };
  const loadSubs = async () => {
    const list = await get(`/sessions/${sid}/submissions`, { problem_id: pid, pageSize: 100 });
    subs.clear();
    for (const x of list.data) subs.set(x.id, x);
    drawSubs();
  };
  subsBox.onclick = (e) => {
    const b = e.target.closest('[data-view]');
    if (b) busy(b, () => viewSubmission(b.dataset.view));
  };
  ctx.on('run.updated', (x) => {
    if (String(x.session_id) !== String(sid)) return;
    if (String(x.problem_id) === String(pid)) upsert(x);
    loadStates().catch(() => {});
    if (x.result && x.result.status === 'completed' && x.problem_id && String(x.problem_id) === String(pid)) {
      // Small cue when one of my submissions finishes.
      const r = x.result;
      if (r.verdict) toast(`Bài #${x.id}: ${r.verdict === 'CE' ? 'lỗi biên dịch' : `${r.passed_tests}/${r.total_tests} ${r.verdict}`}`, r.verdict === 'AC' ? 'ok' : '');
    }
  });
  ctx.on('resync', () => { loadSubs().catch(() => {}); loadStates().catch(() => {}); loadAnns(false).catch(() => {}); });

  await Promise.all([loadSubs(), loadStates()]);
}

async function viewSubmission(id) {
  const [s, src] = await Promise.all([
    get(`/submissions/${id}`),
    request('GET', `/submissions/${id}/source`, { as: 'text' }),
  ]);
  const r = s.result || {};
  modal(html`
    <div class="row"><h3 style="margin:0">Bài nộp #${s.id} — ${s.problem_code}</h3>${resultBadge(s.result)}</div>
    <p class="small muted">Nộp lúc ${fmtTime(s.received_at)}${r.finished_at ? ` · chấm xong ${fmtTime(r.finished_at)}` : ''}</p>
    ${r.compile_output ? html`<h3>Thông báo biên dịch</h3><pre>${r.compile_output}</pre>` : ''}
    <h3>Mã nguồn</h3><pre>${src}</pre>`);
}

// Re-run the current route (the router cleans up timers and handlers first).
function reloadPage() { window.dispatchEvent(new HashChangeEvent('hashchange')); }
