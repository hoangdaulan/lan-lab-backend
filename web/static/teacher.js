// Teacher pages: students, exams (problems + sessions), live session board,
// submissions, announcements, queue.
import { get, post, put, del, upload, request } from './api.js';
import * as live from './live.js';
import * as screen from './screen.js';
import {
  html, raw, mount, $, $$, busy, toast, modal, showError, errText, fmtTime, fmtClock,
  toLocalInput, fromLocalInput, stateBadge, resultBadge, resultText, pager, formData, downloadBlob,
} from './ui.js';

export const teacherNav = [
  ['/exams', 'Đề thi'],
  ['/students', 'Sinh viên'],
  ['/queue', 'Hàng đợi chấm'],
];

export const teacherRoutes = [
  [/^\/students$/, studentsPage],
  [/^\/exams$/, examsPage],
  [/^\/exams\/(\d+)$/, examPage],
  [/^\/sessions\/(\d+)(?:\/(\w+))?$/, sessionPage],
  [/^\/submissions\/(\d+)$/, submissionPage],
  [/^\/queue$/, queuePage],
];

// ---------------------------------------------------------------- students

async function studentsPage(root) {
  let query = '', page = 1;
  mount(root, html`
    <div class="page-heading"><div><span class="breadcrumb">Giảng dạy / Quản lý lớp</span><h1>Sinh viên</h1><p>Quản lý danh sách lớp và tài khoản sinh viên.</p></div></div>
    <div class="grid2">
      <div class="panel"><h3>Thêm một sinh viên</h3>
        <form class="inline" id="add">
          <label>MSV<input name="code" required></label>
          <label>Họ tên<input name="fullname" required></label>
          <button class="primary">Thêm</button>
        </form>
        <p class="muted small">Mật khẩu ban đầu = MSV.</p>
      </div>
      <div class="panel"><h3>Nhập danh sách CSV</h3>
        <form class="inline" id="imp">
          <input type="file" name="file" accept=".csv,text/csv" required>
          <button class="primary">Nhập CSV</button>
        </form>
        <p class="muted small">Dòng đầu: <code>code,full_name</code>. Có dòng lỗi thì không lưu gì; MSV đã có thì bỏ qua.</p>
        <div id="imp-result"></div>
      </div>
    </div>
    <div class="panel">
      <form class="inline" id="search"><label class="search-field"><input name="q" type="search" aria-label="Tìm sinh viên theo mã hoặc họ tên" placeholder="Tìm theo mã sinh viên hoặc họ tên…"></label><button>Tìm kiếm</button></form>
      <div id="list"></div>
    </div>`);

  const load = async () => {
    const list = await get('/students', { query, page, pageSize: 50 });
    const box = $('#list');
    mount(box, html`<table><tr><th>MSV</th><th>Họ tên</th><th>Tạo lúc</th><th></th></tr>
      ${list.data.map((s) => html`<tr><td>${s.code}</td><td>${s.fullname}</td><td class="small">${fmtTime(s.created_at)}</td>
        <td><button data-reset="${s.id}" data-code="${s.code}">Đặt lại mật khẩu</button></td></tr>`)}
      ${list.data.length ? '' : html`<tr><td colspan="4" class="muted">Không có sinh viên</td></tr>`}</table>`);
    box.append(pager(list, (p) => { page = p; load().catch(showError); }));
  };

  $('#search').onsubmit = (e) => { e.preventDefault(); query = e.target.q.value.trim(); page = 1; load().catch(showError); };
  $('#list').onclick = (e) => {
    const b = e.target.closest('[data-reset]');
    if (!b || !confirm(`Đặt lại mật khẩu của ${b.dataset.code} về MSV?`)) return;
    busy(b, async () => { await post(`/students/${b.dataset.reset}/reset-password`); toast('Đã đặt lại mật khẩu về MSV', 'ok'); });
  };
  $('#add').onsubmit = (e) => {
    e.preventDefault();
    const f = e.target;
    busy(f.querySelector('button'), async () => {
      await post('/students', formData(f));
      toast('Đã thêm sinh viên', 'ok');
      f.reset();
      await load();
    });
  };
  $('#imp').onsubmit = (e) => {
    e.preventDefault();
    const f = e.target, out = $('#imp-result');
    const fd = new FormData();
    fd.append('file', f.file.files[0]);
    busy(f.querySelector('button'), async () => {
      try {
        const r = await upload('/students/import', fd);
        mount(out, html`<p class="badge ok">Thêm ${r.created_count}, bỏ qua ${r.skipped_count}</p>
          ${r.skipped.length ? html`<ul class="small">${r.skipped.map((s) => html`<li>Dòng ${s.line} (${s.code}): ${s.reason}</li>`)}</ul>` : ''}`);
        f.reset();
        await load();
      } catch (err) {
        const rows = (err.body && err.body.row_errors) || [];
        mount(out, html`<div class="errors">${errText(err)}
          ${rows.length ? html`<ul>${rows.map((r) => html`<li>Dòng ${r.line} (${r.code}): ${r.error}</li>`)}</ul>` : ''}</div>`);
      }
    });
  };
  await load();
}

// ---------------------------------------------------------------- exams

async function examsPage(root) {
  const list = await get('/exams', { pageSize: 200 });
  mount(root, html`
    <div class="page-heading"><div><span class="breadcrumb">Giảng dạy / Thư viện</span><h1>Đề thi</h1><p>Chuẩn bị bài tập, quản lý bộ test và tổ chức phiên thi.</p></div><span class="page-meta">${list.data.length} đề thi</span></div>
    <div class="panel create-exam"><div class="form-intro"><h3>Tạo đề mới</h3><p>Thêm đề thi, sau đó thiết lập bài tập và thời gian làm bài.</p></div>
      <form class="inline" id="new">
        <label>Tên đề<input name="title" required maxlength="255" size="40"></label>
        <label>Mô tả<input name="description" size="40"></label>
        <button class="primary">Tạo đề thi</button>
      </form>
    </div>
    <div class="section-heading"><h2>Thư viện đề thi</h2><span class="muted">Chọn đề để quản lý</span></div>
    <table class="exam-table"><tr><th>Mã đề</th><th>Tên đề</th><th>Mô tả</th><th>Cập nhật</th></tr>
      ${list.data.map((e) => html`<tr class="clickable" data-href="#/exams/${e.id}"><td>${e.id}</td><td><a href="#/exams/${e.id}">${e.title}</a></td>
        <td class="muted">${e.description}</td><td class="small">${fmtTime(e.updated_at)}</td></tr>`)}
      ${list.data.length ? '' : html`<tr><td colspan="4" class="muted">Chưa có đề</td></tr>`}
    </table>`);
  rowLinks(root);
  $('#new').onsubmit = (e) => {
    e.preventDefault();
    busy(e.target.querySelector('button'), async () => {
      const ex = await post('/exams', formData(e.target));
      location.hash = `#/exams/${ex.id}`;
    });
  };
}

function rowLinks(root) {
  root.addEventListener('click', (e) => {
    if (e.target.closest('a,button,input')) return;
    const tr = e.target.closest('tr[data-href]');
    if (tr) location.hash = tr.dataset.href;
  });
}

async function examPage(root, [id]) {
  const [exam, problems, sessions] = await Promise.all([
    get(`/exams/${id}`), get(`/exams/${id}/problems`, { pageSize: 200 }), get(`/exams/${id}/sessions`, { pageSize: 200 }),
  ]);
  mount(root, html`
    <p><a href="#/exams">← Đề thi</a></p>
    <div class="page-heading"><div><span class="breadcrumb">Thư viện / Đề #${exam.id}</span><h1>${exam.title}</h1><p>${exam.description || 'Quản lý bài tập và phiên thi.'}</p></div></div>
    <div class="panel">
      <form class="inline" id="exam">
        <label>Tên đề<input name="title" value="${exam.title}" required size="40"></label>
        <label>Mô tả<input name="description" value="${exam.description}" size="40"></label>
        <button>Lưu</button><span class="spacer"></span>
        <button type="button" class="danger" id="del-exam">Xóa đề</button>
      </form>
    </div>

    <div class="exam-section-heading"><h2>Bài tập</h2><span>${problems.data.length} bài</span><span class="spacer"></span><label class="search-field"><input id="problem-search" type="search" placeholder="Tìm bài tập…" aria-label="Tìm bài tập theo mã hoặc tên"></label></div>
    <div class="panel">
      <table><tr><th>Mã</th><th>Tên</th><th>Giới hạn</th><th>Version</th><th>Test</th><th>Thư mục test trên máy chủ</th><th></th></tr>
      ${problems.data.map((p) => html`<tr data-problem-row="${p.id}" data-search="${p.code} ${p.title}">
        <td><b>${p.code}</b></td><td>${p.title}</td><td class="small">${p.time_limit_ms} ms · ${p.memory_limit_mb} MB</td>
        <td>v${p.version}</td><td class="small" data-teststate="${p.id}">…</td><td><code class="small">./data/${p.test_dir}</code></td>
        <td class="row"><button data-edit="${p.id}">Sửa</button><button data-tests="${p.id}">Test</button>
          <button class="primary" data-rejudge="${p.id}" data-code="${p.code}">Quét test &amp; chấm lại</button>
          <button class="danger" data-del="${p.id}" data-code="${p.code}">Xóa</button></td></tr>`)}
      ${problems.data.length ? '' : html`<tr><td colspan="7" class="muted">Chưa có bài</td></tr>`}
      </table>
      <p><button class="primary" id="new-problem">+ Thêm bài</button></p>
      <p class="muted small">Sau khi tạo bài: chép <code>1.in, 1.out, 2.in, 2.out…</code> vào thư mục test trên máy chủ rồi bấm
        <b>Quét test &amp; chấm lại</b>. Sửa test giữa giờ cũng vậy — mọi bài đã nộp sẽ được chấm lại theo test mới.</p>
    </div>

    <div class="exam-section-heading"><h2>Phiên thi</h2><span>${sessions.data.length} phiên</span></div>
    <div class="panel">
      <table><tr><th>#</th><th>Tên phiên</th><th>Bắt đầu</th><th>Kết thúc</th><th>Trạng thái</th><th></th></tr>
      ${sessions.data.map((s) => html`<tr>
        <td>${s.id}</td><td><a href="#/sessions/${s.id}">${s.name}</a></td><td>${fmtTime(s.starts_at)}</td><td>${fmtTime(s.ends_at)}</td>
        <td>${stateBadge(s.state)}</td>
        <td class="row"><a class="btn" href="#/sessions/${s.id}">Mở bảng</a><button data-sedit="${s.id}">Sửa</button>
          <button class="danger" data-sdel="${s.id}">Xóa</button></td></tr>`)}
      ${sessions.data.length ? '' : html`<tr><td colspan="6" class="muted">Chưa có phiên</td></tr>`}
      </table>
      <p><button class="primary" id="new-session">+ Thêm phiên</button></p>
    </div>`);

  const reload = () => examPage(root, [id]).catch(showError);
  $('#problem-search', root).oninput = (e) => {
    const q = e.target.value.trim().toLocaleLowerCase('vi');
    for (const row of root.querySelectorAll('[data-problem-row]')) row.hidden = !row.dataset.search.toLocaleLowerCase('vi').includes(q);
  };
  for (const p of problems.data) {
    get(`/problems/${p.id}/tests`, { pageSize: 200 })
      .then((r) => mount(root.querySelector(`[data-teststate="${p.id}"]`), testState(r)))
      .catch(() => {});
  }
  const byId = (list, pid) => list.data.find((x) => String(x.id) === String(pid));

  $('#exam').onsubmit = (e) => {
    e.preventDefault();
    busy(e.target.querySelector('button'), async () => { await put(`/exams/${id}`, formData(e.target)); toast('Đã lưu', 'ok'); });
  };
  $('#del-exam').onclick = (e) => {
    if (!confirm('Xóa đề này? (Chỉ được khi đề không còn bài và phiên)')) return;
    busy(e.target, async () => { await del(`/exams/${id}`); location.hash = '#/exams'; });
  };
  $('#new-problem').onclick = () => problemForm(id, null, reload);
  $('#new-session').onclick = () => sessionForm(id, null, reload);

  root.onclick = (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    const d = b.dataset;
    if (d.edit) problemForm(id, byId(problems, d.edit), reload);
    else if (d.tests) testsModal(d.tests);
    else if (d.rejudge) {
      if (!confirm(`Quét lại thư mục test của bài ${d.code} và chấm lại MỌI bài đã nộp?`)) return;
      busy(b, async () => {
        try {
          const r = await post(`/problems/${d.rejudge}/rejudge`);
          toast(`Bài ${d.code}: ${r.tests.length} test, version ${r.problem.version}. Đã xếp hàng chấm lại ${r.requeued_submissions} bài nộp.`, 'ok');
          reload();
        } catch (err) {
          modal(html`<h3>Không quét được test của bài ${d.code}</h3><p class="errors">${errText(err)}</p>
            <p class="muted small">Mỗi <code>N.in</code> cần có <code>N.out</code> (và ngược lại); không dùng thư mục con hay symlink. Không có gì bị thay đổi.</p>`);
        }
      });
    } else if (d.del) {
      if (!confirm(`Xóa bài ${d.code}? (Chỉ được khi chưa có bài nộp)`)) return;
      busy(b, async () => { await del(`/problems/${d.del}`); reload(); });
    } else if (d.sedit) sessionForm(id, byId(sessions, d.sedit), reload);
    else if (d.sdel) {
      if (!confirm('Xóa phiên này? (Chỉ được khi chưa có bài nộp)')) return;
      busy(b, async () => { await del(`/sessions/${d.sdel}`); reload(); });
    }
  };
}

// Registered tests vs. the files on disk (GET /problems/{id}/tests "disk").
function testState(r) {
  const n = r.data.length, d = r.disk || {};
  const scan = 'bấm "Quét test & chấm lại"';
  if (n === 0) {
    if (d.tests > 0) return html`<span class="badge bad">Chưa có test</span><div class="errors">Có ${d.tests} cặp file chưa quét — ${scan}</div>`;
    return html`<span class="badge bad">Chưa có test</span><div class="errors">Chép file vào thư mục rồi ${scan}${d.error ? html`<br><span class="muted">${errText({ message: d.error })}</span>` : ''}</div>`;
  }
  if (d.in_sync) return html`<span class="badge ok">${n} test</span>`;
  if (d.error) return html`<span class="badge wait">${n} test</span><div class="errors">Thư mục đang lỗi, quét sẽ thất bại:<br>${errText({ message: d.error })}</div>`;
  return html`<span class="badge wait">${n} test</span><div class="errors">Trên đĩa có ${d.tests} cặp khác với test đang dùng — ${scan}</div>`;
}

function problemForm(examId, p, done) {
  const m = modal(html`
    <h3>${p ? `Sửa bài ${p.code}` : 'Thêm bài'}</h3>
    <form class="stack" style="max-width:none">
      <div class="row">
        <label>Mã bài<input name="code" value="${p ? p.code : ''}" required maxlength="20" size="6"></label>
        <label style="flex:1">Tên bài<input name="title" value="${p ? p.title : ''}" required></label>
      </div>
      <label>Đề bài<textarea name="statement" rows="12">${p ? p.statement : ''}</textarea></label>
      <div class="row">
        <label>Giới hạn thời gian (ms)<input name="time_limit_ms" type="number" min="1" max="10000" value="${p ? p.time_limit_ms : 1000}" required></label>
        <label>Giới hạn bộ nhớ (MB)<input name="memory_limit_mb" type="number" min="1" max="1024" value="${p ? p.memory_limit_mb : 256}" required></label>
      </div>
      ${p ? html`<p class="muted small">Đổi giới hạn thời gian/bộ nhớ sẽ chấm lại mọi bài đã nộp của bài này.</p>` : ''}
      <div class="row end"><button type="button" data-close>Hủy</button><button class="primary">Lưu</button></div>
    </form>`);
  const f = $('form', m.root);
  $('[data-close]', m.root).onclick = m.close;
  f.onsubmit = (e) => {
    e.preventDefault();
    const body = formData(f);
    body.time_limit_ms = Number(body.time_limit_ms);
    body.memory_limit_mb = Number(body.memory_limit_mb);
    busy(f.querySelector('button.primary'), async () => {
      if (p) {
        const r = await put(`/problems/${p.id}`, body);
        toast(r.rejudge_queued ? `Đã lưu; chấm lại ${r.requeued_submissions} bài nộp` : 'Đã lưu', 'ok');
      } else {
        const r = await post(`/exams/${examId}/problems`, body);
        toast(`Đã tạo bài ${r.code}. Chép test vào ./data/${r.test_dir} rồi bấm "Quét test & chấm lại".`, 'ok');
      }
      m.close();
      done();
    });
  };
}

async function testsModal(pid) {
  const r = await get(`/problems/${pid}/tests`, { pageSize: 200 }).catch(showError);
  if (!r) return;
  modal(html`<h3>Test đang dùng (${r.data.length})</h3>
    <p class="small">Thư mục: <code>./data/${r.test_dir}</code></p>
    <table><tr><th>#</th><th>Input</th><th>Output</th></tr>
    ${r.data.map((t, i) => html`<tr><td>${i + 1}</td><td><code>${t.input}</code></td><td><code>${t.output}</code></td></tr>`)}
    ${r.data.length ? '' : html`<tr><td colspan="3" class="muted">Chưa có test — chép file vào thư mục rồi bấm "Quét test &amp; chấm lại".</td></tr>`}
    </table>`);
}

function sessionForm(examId, s, done) {
  const start = s ? s.starts_at : new Date(Date.now() + 5 * 60_000).toISOString();
  const end = s ? s.ends_at : new Date(Date.now() + 95 * 60_000).toISOString();
  const m = modal(html`
    <h3>${s ? 'Sửa phiên' : 'Thêm phiên'}</h3>
    <form class="stack">
      <label>Tên phiên<input name="name" value="${s ? s.name : 'Ca 1'}" required></label>
      <label>Bắt đầu<input name="starts_at" type="datetime-local" value="${toLocalInput(start)}" required></label>
      <label>Kết thúc<input name="ends_at" type="datetime-local" value="${toLocalInput(end)}" required></label>
      <p class="muted small">Giờ theo máy của bạn; máy chủ dùng giờ của nó để mở/đóng nộp bài.</p>
      <div class="row end"><button type="button" data-close>Hủy</button><button class="primary">Lưu</button></div>
    </form>`);
  const f = $('form', m.root);
  $('[data-close]', m.root).onclick = m.close;
  f.onsubmit = (e) => {
    e.preventDefault();
    const body = { name: f.name.value, starts_at: fromLocalInput(f.starts_at.value), ends_at: fromLocalInput(f.ends_at.value) };
    busy(f.querySelector('button.primary'), async () => {
      if (s) await put(`/sessions/${s.id}`, body);
      else await post(`/exams/${examId}/sessions`, body);
      m.close();
      done();
    });
  };
}

// ---------------------------------------------------------------- session

const TABS = [['board', 'Bảng trực tiếp'], ['screens', 'Màn hình SV'], ['submissions', 'Bài nộp'], ['participants', 'Người tham gia'], ['announcements', 'Thông báo']];

async function sessionPage(root, [id, tab = 'board'], ctx) {
  const info = await get(`/sessions/${id}`);
  const s = info.session;
  live.subscribe(Number(id));
  mount(root, html`
    <p><a href="#/exams/${s.exam_id}">← ${s.exam_title}</a></p>
    <span class="breadcrumb">Điều hành phiên thi</span>
    <div class="row"><h1 style="margin:0">${s.name}</h1>${stateBadge(s.state)}
      <span class="muted small">${fmtTime(s.starts_at)} → ${fmtTime(s.ends_at)}</span><span class="spacer"></span>
      <button id="csv">Xuất CSV</button></div>
    <div class="tabs" style="margin-top:10px">${TABS.map(([k, label]) =>
      html`<a href="#/sessions/${id}/${k}" class="${k === tab ? 'active' : ''}">${label}</a>`)}</div>
    <div id="tab"></div>`);
  $('#csv').onclick = (e) => busy(e.target, async () => {
    downloadBlob(await request('GET', `/sessions/${id}/export.csv`, { as: 'blob' }), `session-${id}-summary.csv`);
  });
  const box = $('#tab');
  if (tab === 'board') return boardTab(box, id, info, ctx);
  if (tab === 'submissions') return submissionsTab(box, id, info, ctx);
  if (tab === 'participants') return participantsTab(box, id, ctx);
  if (tab === 'announcements') return announcementsTab(box, id, ctx);
  if (tab === 'screens') return screensTab(box, id, ctx);
}

// Students working in the session with their screen-sharing state; click to
// watch a screen live (WebRTC peer to peer, see screen.js).
async function screensTab(box, id, ctx) {
  mount(box, html`
    <div class="row small" style="margin-bottom:8px"><span id="scr-sum" class="muted"></span></div>
    <table><tr><th>MSV</th><th>Họ tên</th><th>Bài làm</th><th>Màn hình</th><th></th></tr><tbody id="scr-rows"></tbody></table>
    <p class="muted small">Sinh viên phải chia sẻ toàn màn hình mới làm bài được. Video đi thẳng từ máy sinh viên tới máy này trong LAN; xem tối đa 4 màn hình cùng lúc.</p>
    <div class="screens" id="scr-tiles"></div>`);
  let participants = [];
  let online = new Map(); // student_id -> sharing
  const tiles = new Map(); // student_id -> {el, stop}

  const draw = () => {
    const rows = participants.map((p) => ({ ...p, conn: online.has(p.student_id), sharing: online.get(p.student_id) === true }));
    const sharing = rows.filter((r) => r.sharing).length;
    $('#scr-sum').textContent = `${sharing} đang chia sẻ · ${rows.filter((r) => r.conn).length} đang mở trang · ${rows.length} đã tham gia`;
    mount($('#scr-rows'), rows.map((r) => html`<tr>
      <td><span class="dot ${r.conn ? 'on' : ''}"></span>${r.student_code}</td><td>${r.student_fullname}</td>
      <td class="small">${r.status === 'finished' ? 'Đã kết thúc' : 'Đang làm'}</td>
      <td>${r.sharing ? html`<span class="badge ok">Đang chia sẻ</span>` : r.conn ? html`<span class="badge bad">Chưa/đã tắt chia sẻ</span>` : html`<span class="badge">Offline</span>`}</td>
      <td>${r.sharing ? html`<button data-watch="${r.student_id}" ${tiles.has(r.student_id) ? raw('disabled') : ''}>Xem màn hình</button>` : ''}</td></tr>`));
    if (!rows.length) mount($('#scr-rows'), html`<tr><td colspan="5" class="muted">Chưa có sinh viên tham gia</td></tr>`);
  };
  const name = (sid) => { const p = participants.find((x) => x.student_id === sid); return p ? `${p.student_code} — ${p.student_fullname}` : `SV #${sid}`; };

  const closeTile = (sid) => {
    const t = tiles.get(sid);
    if (!t) return;
    tiles.delete(sid);
    t.stop();
    t.el.remove();
    draw();
  };
  const openTile = (sid) => {
    if (tiles.has(sid)) return;
    const el = document.createElement('div');
    el.className = 'screen-tile';
    mount(el, html`<div class="row"><b>${name(sid)}</b><span class="spacer"></span><span class="small" data-st>Đang kết nối…</span>
      <button data-close>Đóng</button></div><video autoplay playsinline muted></video>`);
    $('#scr-tiles').append(el);
    const video = el.querySelector('video'), st = el.querySelector('[data-st]');
    el.querySelector('[data-close]').onclick = () => closeTile(sid);
    video.ondblclick = () => video.requestFullscreen && video.requestFullscreen();
    const stop = screen.watch(sid, {
      onTrack: (ms) => { video.srcObject = ms; st.textContent = 'Trực tiếp (nhấp đúp để phóng to)'; },
      onEnd: () => { st.textContent = 'Đã dừng'; setTimeout(() => closeTile(sid), 1500); },
      onError: (msg) => { toast(`${name(sid)}: ${errText({ message: msg })}`, 'error'); closeTile(sid); },
    });
    tiles.set(sid, { el, stop });
    draw();
  };

  box.addEventListener('click', (e) => {
    const b = e.target.closest('[data-watch]');
    if (b) openTile(Number(b.dataset.watch));
  });
  ctx.on('screen.list', (d) => {
    if (String(d.session_id) !== String(id)) return;
    online = new Map(d.students.map((x) => [x.student_id, x.sharing]));
    for (const sid of [...tiles.keys()]) if (online.get(sid) !== true) closeTile(sid);
    draw();
  });
  const loadParticipants = async () => { participants = (await get(`/sessions/${id}/participants`, { pageSize: 200 })).data; draw(); };
  ctx.on('resync', () => loadParticipants().catch(() => {}));
  ctx.every(15_000, () => loadParticipants().catch(() => {}));
  ctx.onCleanup(() => { for (const sid of [...tiles.keys()]) closeTile(sid); });
  await loadParticipants();
  // Ask for the current list (the page may have subscribed before this tab).
  live.send('subscribe', { session_id: Number(id) });
}

function boardTab(box, id, info, ctx) {
  const draw = (sum) => {
    const ps = sum.problems || [];
    mount(box, html`
      <div class="board-status row small">
        <span><span class="dot on"></span>Online: <b>${sum.online_count}</b></span>
        <span>Tham gia: <b>${sum.rows.length}</b></span>
        <span>Hàng đợi: <b>${sum.queue.queued}</b> chờ · <b>${sum.queue.running}</b> đang chấm</span>
        <span class="spacer"></span><span class="muted">cập nhật ${fmtClock(sum.generated_at)}</span>
      </div>
      <table class="board">
        <tr><th>MSV</th><th>Họ tên</th><th>Trạng thái</th>${ps.map((p) => html`<th title="${p.title}">${p.code}</th>`)}<th>AC</th></tr>
        ${sum.rows.map((r) => html`<tr>
          <td><span class="dot ${r.online ? 'on' : ''}"></span>${r.student_code}</td><td>${r.student_fullname}</td>
          <td class="small">${r.status === 'finished' ? 'Đã nộp xong' : 'Đang làm'}</td>
          ${r.cells.map((c) => html`<td class="cell ${c.state}" data-student="${r.student_id}" data-problem="${c.problem_id}"
              title="${c.submissions} lần nộp">${cellText(c)}</td>`)}
          <td><b>${r.ac_count}/${sum.problems_total}</b></td></tr>`)}
        ${sum.rows.length ? '' : html`<tr><td colspan="${ps.length + 4}" class="muted">Chưa có sinh viên tham gia</td></tr>`}
      </table>
      <p class="muted small">Bấm vào một ô để xem các bài nộp. Bảng tự cập nhật qua WebSocket.</p>`);
  };
  const load = () => get(`/sessions/${id}/summary`).then(draw).catch(showError);
  box.onclick = (e) => {
    const td = e.target.closest('td[data-student]');
    if (td) location.hash = `#/sessions/${id}/submissions?student=${td.dataset.student}&problem=${td.dataset.problem}`;
  };
  ctx.on('summary.updated', (sum) => { if (String(sum.session_id) === String(id)) draw(sum); });
  ctx.on('resync', load);
  return load();
}

function cellText(c) {
  if (c.state === 'none') return '';
  if (c.state === 'ac') return `AC (${c.submissions})`;
  if (c.state === 'pending') return 'đang chấm…';
  return c.latest ? resultText({ status: c.latest.status, verdict: c.latest.verdict, passed_tests: c.latest.passed_tests, total_tests: c.latest.total_tests }) : '—';
}

async function submissionsTab(box, id, info, ctx) {
  // Filters arrive from the board as ?student=&problem= in the hash.
  const q = new URLSearchParams(location.hash.split('?')[1] || '');
  let studentId = q.get('student') || '', problemId = q.get('problem') || '', page = 1;
  const participants = await get(`/sessions/${id}/participants`, { pageSize: 200 });
  mount(box, html`
    <div class="section-heading"><h2>Bài nộp của phiên</h2><span class="muted">Kết quả cập nhật trực tiếp</span></div>
    <form class="inline" id="flt">
      <label>Sinh viên<select name="student"><option value="">Tất cả</option>
        ${participants.data.map((p) => html`<option value="${p.student_id}" ${String(p.student_id) === studentId ? raw('selected') : ''}>${p.student_code} — ${p.student_fullname}</option>`)}</select></label>
      <label>Bài<select name="problem"><option value="">Tất cả</option>
        ${info.problems.map((p) => html`<option value="${p.id}" ${String(p.id) === problemId ? raw('selected') : ''}>${p.code} — ${p.title}</option>`)}</select></label>
    </form>
    <div id="subs"></div>`);
  const rows = new Map();
  const load = async () => {
    const list = await get(`/sessions/${id}/submissions`, { student_id: studentId, problem_id: problemId, page, pageSize: 50 });
    rows.clear();
    const el = $('#subs');
    mount(el, html`<table><tr><th>#</th><th>Lúc nộp</th><th>Sinh viên</th><th>Bài</th><th>Kết quả</th><th>v</th></tr>
      ${list.data.map((s) => subRow(s))}
      ${list.data.length ? '' : html`<tr><td colspan="6" class="muted">Chưa có bài nộp</td></tr>`}</table>`);
    el.append(pager(list, (p) => { page = p; load().catch(showError); }));
  };
  const subRow = (s) => html`<tr class="clickable" data-href="#/submissions/${s.id}" data-sub="${s.id}">
    <td><a href="#/submissions/${s.id}">${s.id}</a></td><td class="small">${fmtTime(s.received_at)}</td>
    <td>${s.student_code} — ${s.student_fullname}</td><td>${s.problem_code}</td>
    <td class="res">${resultBadge(s.result)}</td><td class="small">${s.problem_version}</td></tr>`;
  rowLinks(box);
  $('#flt').onchange = (e) => {
    const f = e.currentTarget;
    studentId = f.student.value; problemId = f.problem.value; page = 1;
    load().catch(showError);
  };
  ctx.on('run.updated', (s) => {
    if (String(s.session_id) !== String(id)) return;
    const tr = box.querySelector(`tr[data-sub="${s.id}"]`);
    if (tr) mount(tr.querySelector('.res'), resultBadge(s.result));
    else if (page === 1 && (!studentId || String(s.student_id) === studentId) && (!problemId || String(s.problem_id) === problemId)) load().catch(showError);
  });
  ctx.on('resync', () => load().catch(showError));
  await load();
}

async function participantsTab(box, id, ctx) {
  const load = async () => {
    const list = await get(`/sessions/${id}/participants`, { pageSize: 200 });
    mount(box, html`<div class="section-heading"><h2>Người tham gia</h2><span class="muted">${list.data.length} sinh viên · cập nhật mỗi 10 giây</span></div><table><tr><th>MSV</th><th>Họ tên</th><th>Trạng thái</th><th>Vào lúc</th><th>Kết thúc lúc</th></tr>
      ${list.data.map((p) => html`<tr><td>${p.student_code}</td><td>${p.student_fullname}</td>
        <td>${p.status === 'finished' ? html`<span class="badge">Đã kết thúc</span>` : html`<span class="badge ok">Đang làm</span>`}</td>
        <td class="small">${fmtTime(p.joined_at)}</td><td class="small">${fmtTime(p.finished_at)}</td></tr>`)}
      ${list.data.length ? '' : html`<tr><td colspan="5" class="muted">Chưa ai tham gia</td></tr>`}</table>`);
  };
  ctx.every(10_000, () => load().catch(() => {}));
  await load();
}

async function announcementsTab(box, id, ctx) {
  mount(box, html`
    <div class="section-heading"><h2>Thông báo của phiên</h2><span class="muted">Gửi trực tiếp đến sinh viên</span></div>
    <div class="panel"><form class="inline" id="ann">
      <label style="flex:1">Nội dung<input name="text" maxlength="1000" required placeholder="Ví dụ: Còn 10 phút"></label>
      <button class="primary">Gửi tới cả phiên</button></form>
      <p class="muted small">Sinh viên đang mở trang sẽ thấy ngay. Thông báo lưu trong bộ nhớ máy chủ (mất khi khởi động lại API).</p></div>
    <div id="anns"></div>`);
  const load = async () => {
    const r = await get(`/sessions/${id}/announcements`);
    mount($('#anns'), r.data.length
      ? r.data.map((a) => html`<div class="announce"><span class="muted small">${fmtClock(a.created_at)}</span> ${a.text}</div>`)
      : html`<p class="muted">Chưa có thông báo</p>`);
  };
  $('#ann').onsubmit = (e) => {
    e.preventDefault();
    const f = e.target;
    busy(f.querySelector('button'), async () => { await post(`/sessions/${id}/announcements`, { text: f.text.value }); f.reset(); });
  };
  ctx.on('announcement', () => load().catch(showError));
  ctx.on('resync', () => load().catch(showError));
  await load();
}

// ---------------------------------------------------------------- submission

async function submissionPage(root, [id], ctx) {
  const draw = async () => {
    const [s, src, tests] = await Promise.all([
      get(`/submissions/${id}`), request('GET', `/submissions/${id}/source`, { as: 'text' }), get(`/submissions/${id}/tests`, { pageSize: 200 }),
    ]);
    live.subscribe(s.session_id);
    const r = s.result || {};
    mount(root, html`
      <p><a href="#/sessions/${s.session_id}/submissions">← Bài nộp của phiên</a></p>
      <span class="breadcrumb">Kết quả chấm / ${s.problem_code} / #${s.id}</span>
      <div class="row"><h1 style="margin:0">Bài nộp #${s.id}</h1>${resultBadge(s.result)}<span class="spacer"></span>
        <button id="rj">Chấm lại bài này</button></div>
      <div class="panel" style="margin-top:10px"><dl class="kv">
        <dt>Sinh viên</dt><dd>${s.student_code} — ${s.student_fullname}</dd>
        <dt>Bài</dt><dd>${s.problem_code} (version ${s.problem_version})</dd>
        <dt>Nộp lúc</dt><dd>${fmtTime(s.received_at)}</dd>
        <dt>Chấm</dt><dd>${r.started_at ? fmtTime(r.started_at) : '—'} → ${r.finished_at ? fmtTime(r.finished_at) : '—'}</dd>
        ${r.compiler_info ? html`<dt>Trình biên dịch</dt><dd class="small">${r.compiler_info}</dd>` : ''}
      </dl></div>
      ${r.system_error ? html`<h3>Lỗi hệ thống</h3><pre>${r.system_error}</pre>` : ''}
      ${r.compile_output ? html`<h3>Thông báo biên dịch</h3><pre>${r.compile_output}</pre>` : ''}
      <div class="grid2">
        <div><h3>Mã nguồn</h3><pre>${src}</pre></div>
        <div><h3>Kết quả từng test</h3>
          <table><tr><th>#</th><th>Kết quả</th><th>Thời gian</th><th>Bộ nhớ</th></tr>
          ${tests.data.map((t, i) => html`<tr><td>${i + 1}</td>
            <td><span class="badge ${t.verdict === 'AC' ? 'ok' : 'bad'}">${t.verdict}</span></td>
            <td class="mono">${t.execution_time_ms} ms</td><td class="mono">${t.memory_used_kb != null ? `${(t.memory_used_kb / 1024).toFixed(2)} MB` : '—'}</td></tr>`)}
          ${tests.data.length ? '' : html`<tr><td colspan="4" class="muted">Chưa có kết quả test</td></tr>`}
          </table>
          <p class="muted small">Dừng ở test sai đầu tiên nên chỉ các test đã chạy có dòng.</p></div>
      </div>`);
    $('#rj').onclick = (e) => busy(e.target, async () => { await post(`/submissions/${id}/rejudge`); toast('Đã xếp hàng chấm lại', 'ok'); draw().catch(showError); });
  };
  ctx.on('run.updated', (s) => { if (String(s.id) === String(id)) draw().catch(showError); });
  ctx.on('resync', () => draw().catch(showError));
  await draw();
}

// ---------------------------------------------------------------- queue

async function queuePage(root, _, ctx) {
  const load = async () => {
    const q = await get('/admin/queue');
    const j = q.jobs || {};
    mount(root, html`
      <div class="page-heading"><div><span class="breadcrumb">Hệ thống / Tiến trình chấm</span><h1>Hàng đợi chấm</h1><p>Theo dõi bài đang xử lý và hoạt động của worker.</p></div><span class="badge info">Tự cập nhật · 3 giây</span></div>
      <div class="queue-layout"><section class="queue-section"><h2>Trạng thái xử lý</h2><div class="queue-meter"><div><span>Đang chờ</span><strong class="queued">${j.queued || 0}</strong></div><div><span>Đang chấm</span><strong class="running">${j.running || 0}</strong></div><div><span>Hoàn tất</span><strong>${j.done || 0}</strong></div></div><dl class="kv"><dt>Worker đang giữ bài</dt><dd class="mono">${(q.active_workers || []).join(', ') || '—'}</dd><dt>Bài chờ lâu nhất</dt><dd class="mono">${q.oldest_queued_ms} ms</dd><dt>Lỗi / đã bỏ</dt><dd class="mono">${j.failed || 0} / ${j.obsolete || 0}</dd></dl></section><section class="queue-section"><h2>10 phút gần nhất</h2><dl class="kv"><dt>Lượt chấm</dt><dd class="mono">${q.recent_runs}</dd><dt>Chờ trung bình</dt><dd class="mono">${Math.round(q.avg_queue_wait_ms)} ms</dd><dt>Chấm trung bình</dt><dd class="mono">${Math.round(q.avg_judge_ms)} ms</dd></dl></section></div><div class="queue-update"><span class="dot on"></span>Cập nhật lúc ${fmtClock(q.generated_at)} · tự làm mới mỗi 3 giây</div>`);
  };
  ctx.every(3000, () => load().catch(() => {}));
  await load();
}
