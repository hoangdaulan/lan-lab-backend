// Small DOM helpers shared by every page.
const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
export const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ESC[c]);

class Raw { constructor(s) { this.s = s; } toString() { return this.s; } }
export const raw = (s) => new Raw(s);

// html`<p>${x}</p>` escapes interpolations; arrays are joined; raw() passes through.
export function html(strings, ...vals) {
  let out = strings[0];
  vals.forEach((v, i) => { out += render(v) + strings[i + 1]; });
  return new Raw(out);
}
function render(v) {
  if (v instanceof Raw) return v.s;
  if (Array.isArray(v)) return v.map(render).join('');
  if (v === null || v === undefined || v === false) return '';
  return esc(v);
}

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

export function mount(node, content) {
  node.innerHTML = Array.isArray(content) ? content.map(String).join('') : String(content);
  return node;
}

export function toast(msg, kind = '') {
  const t = document.createElement('div');
  t.className = 'toast ' + kind;
  t.textContent = msg;
  document.getElementById('toasts').append(t);
  setTimeout(() => t.remove(), kind === 'error' ? 7000 : 4000);
}

// Vietnamese text for the errors students and teachers hit most; anything
// else is shown as the server sent it.
const MSG = {
  'invalid code or password': 'Sai mã hoặc mật khẩu',
  'session has not started': 'Phiên chưa bắt đầu',
  'the session has not started': 'Phiên chưa bắt đầu',
  'session has ended': 'Phiên đã kết thúc',
  'the session has ended': 'Phiên đã hết giờ, không nộp được nữa',
  'join the session first': 'Bạn cần tham gia phiên trước',
  'join the session before submitting': 'Bạn cần tham gia phiên trước khi nộp',
  'you have not joined this session': 'Bạn chưa tham gia phiên này',
  'you have finished this session': 'Bạn đã kết thúc làm bài, không nộp thêm được',
  'the problem has no tests yet': 'Bài này chưa có test, hãy báo giáo viên',
  'the statement is available from the session start time': 'Đề sẽ hiện khi phiên bắt đầu',
  'only .cpp files are accepted': 'Chỉ nhận file .cpp',
  'the file is empty': 'File rỗng',
  'the file is larger than 64 KB': 'File lớn hơn 64 KB',
  "delete the exam's problems and sessions first": 'Xóa hết bài và phiên của đề trước',
  'session has submissions and cannot be deleted': 'Phiên đã có bài nộp nên không xóa được',
  'ends_at must be after starts_at': 'Giờ kết thúc phải sau giờ bắt đầu',
};
// Server errors look like "conflict: the session has ended" (apperr kind first).
const KIND = /^(invalid input|not found|conflict|forbidden|too large): /;
export function errText(e) {
  const m = e && e.message ? e.message : String(e);
  const bare = m.replace(KIND, '');
  return MSG[bare] || bare;
}
export function showError(e) {
  console.error(e);
  let msg = errText(e);
  if (e instanceof TypeError) msg = 'Không kết nối được máy chủ';
  toast(msg, 'error');
}

// Run an async action from a button: disable while running, toast errors.
export async function busy(btn, fn) {
  if (btn) btn.disabled = true;
  try { return await fn(); } catch (e) { showError(e); } finally { if (btn) btn.disabled = false; }
}

export function modal(content) {
  const back = document.createElement('div');
  back.className = 'modal-back';
  back.innerHTML = `<div class="modal">${content}</div>`;
  const close = () => { back.remove(); document.removeEventListener('keydown', onKey); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  back.addEventListener('mousedown', (e) => { if (e.target === back) close(); });
  document.addEventListener('keydown', onKey);
  document.body.append(back);
  return { root: back.firstElementChild, close };
}

export function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleString('vi-VN', { hour12: false });
}
export function fmtClock(iso) {
  if (!iso) return '';
  return new Date(iso).toLocaleTimeString('vi-VN', { hour12: false });
}
// <input type=datetime-local> value <-> ISO (browser local time zone).
export function toLocalInput(iso) {
  const d = iso ? new Date(iso) : new Date();
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
export function fromLocalInput(v) { return new Date(v).toISOString(); }

export function fmtDuration(ms) {
  if (ms < 0) ms = 0;
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60;
  const p = (n) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${p(m)}:${p(x)}` : `${p(m)}:${p(x)}`;
}

export const STATE_VI = { upcoming: 'Sắp diễn ra', running: 'Đang diễn ra', ended: 'Đã kết thúc' };
export function stateBadge(state) {
  const cls = { upcoming: 'info', running: 'ok', ended: '' }[state] ?? '';
  return html`<span class="badge ${cls}">${STATE_VI[state] || state}</span>`;
}

// Result of a submission: "queued", "judging", "10/15 TLE", "CE"...
export function resultText(r) {
  if (!r) return 'chờ chấm lại';
  if (r.status === 'queued') return 'đang chờ';
  if (r.status === 'judging') return 'đang chấm…';
  if (r.status === 'system_error') return 'lỗi hệ thống';
  if (r.verdict === 'CE') return 'CE (lỗi biên dịch)';
  return `${r.passed_tests}/${r.total_tests} ${r.verdict}`;
}
export function resultBadge(r) {
  let cls = 'wait';
  if (r && r.status === 'completed') cls = r.verdict === 'AC' ? 'ok' : 'bad';
  else if (r && r.status === 'system_error') cls = 'bad';
  return html`<span class="badge ${cls}">${resultText(r)}</span>`;
}

// Pager for PagedList {data, page, pageSize, totalPages}.
export function pager(list, onPage) {
  const div = document.createElement('div');
  div.className = 'pager';
  if (!list || list.totalPages <= 1) return div;
  div.innerHTML = String(html`
    <button data-p="${list.page - 1}" ${list.page <= 1 ? raw('disabled') : ''}>‹ Trước</button>
    <span>Trang ${list.page}/${list.totalPages}</span>
    <button data-p="${list.page + 1}" ${list.page >= list.totalPages ? raw('disabled') : ''}>Sau ›</button>`);
  div.addEventListener('click', (e) => {
    const b = e.target.closest('button[data-p]');
    if (b && !b.disabled) onPage(Number(b.dataset.p));
  });
  return div;
}

// Collect named form fields into an object.
export function formData(form) {
  const o = {};
  for (const el of form.elements) if (el.name) o[el.name] = el.value;
  return o;
}

export function downloadBlob(blob, name) {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  document.body.append(a);
  a.click();
  setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
}
