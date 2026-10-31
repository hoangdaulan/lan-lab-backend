// Hash router, login and the shared header. Pages live in teacher.js and student.js.
import { auth, login, logout, resume, onAuthChange, put } from './api.js';
import * as live from './live.js';
import { html, mount, $, busy, toast, errText } from './ui.js';
import { teacherRoutes, teacherNav } from './teacher.js';
import { studentRoutes, studentNav } from './student.js';

const view = document.getElementById('view');
let cleanups = [];

// Page context: register cleanups (timers, WS handlers) run on navigation.
function makeCtx() {
  return {
    onCleanup(fn) { cleanups.push(fn); },
    every(ms, fn) { const t = setInterval(fn, ms); cleanups.push(() => clearInterval(t)); },
    on(type, fn) { cleanups.push(live.on(type, fn)); },
  };
}

const common = [
  [/^\/password$/, passwordPage],
];

function routesFor(role) {
  return [...common, ...(role === 'teacher' ? teacherRoutes : studentRoutes)];
}

async function route() {
  for (const fn of cleanups) { try { fn(); } catch {} }
  cleanups = [];
  const path = (location.hash.replace(/^#/, '') || '/').split('?')[0];
  if (!auth.user) { renderHeader(); return loginPage(); }
  renderHeader(path);
  const routes = routesFor(auth.user.role);
  for (const [re, page] of routes) {
    const m = path.match(re);
    if (m) {
      // A fresh node per page so listeners never leak between pages.
      const node = document.createElement('div');
      view.replaceChildren(node);
      try { await page(node, m.slice(1), makeCtx()); }
      catch (e) { mount(node, html`<div class="panel errors">Lỗi: ${errText(e)}</div>`); console.error(e); }
      return;
    }
  }
  // Unknown path: go to the role's home page.
  location.replace('#' + (auth.user.role === 'teacher' ? '/exams' : '/sessions'));
}

function renderHeader(path = '') {
  const nav = $('#nav'), who = $('#who');
  if (!auth.user) { nav.innerHTML = ''; who.innerHTML = ''; live.stop(); return; }
  const items = auth.user.role === 'teacher' ? teacherNav : studentNav;
  mount(nav, items.map(([href, label]) =>
    html`<a href="#${href}" class="${path.startsWith(href) ? 'active' : ''}">${label}</a>`));
  mount(who, html`<span>${auth.user.fullname} (${auth.user.code} · ${auth.user.role === 'teacher' ? 'GV' : 'SV'})</span>
    <a href="#/password">Đổi mật khẩu</a> <button id="logout">Đăng xuất</button>`);
  $('#logout').onclick = async () => { lastUser = null; await logout(); location.hash = '#/'; route(); };
}

function loginPage() {
  mount(view, html`
    <div class="panel login">
      <h1>Đăng nhập</h1>
      <form class="stack" id="login-form">
        <label>Mã (MSV / mã GV)<input name="code" autocomplete="username" required autofocus></label>
        <label>Mật khẩu<input name="password" type="password" autocomplete="current-password" required></label>
        <button class="primary">Đăng nhập</button>
        <div class="errors" id="login-err"></div>
      </form>
      <p class="muted small">Sinh viên: mật khẩu ban đầu là ngày sinh dạng ddmmyyyy (ví dụ 05/09/2004 → 05092004) — hãy đổi sau khi đăng nhập.</p>
    </div>`);
  const form = $('#login-form');
  form.onsubmit = (e) => {
    e.preventDefault();
    busy(form.querySelector('button'), async () => {
      try {
        await login(form.code.value.trim(), form.password.value);
        location.hash = auth.user.role === 'teacher' ? '#/exams' : '#/sessions';
        route();
      } catch (err) { $('#login-err').textContent = errText(err); }
    });
  };
}

async function passwordPage(root) {
  mount(root, html`
    <div class="panel"><h1>Đổi mật khẩu</h1>
    <form class="stack" id="pw">
      <label>Mật khẩu hiện tại<input name="cur" type="password" required autocomplete="current-password"></label>
      <label>Mật khẩu mới<input name="new1" type="password" required autocomplete="new-password" maxlength="72"></label>
      <label>Nhập lại mật khẩu mới<input name="new2" type="password" required autocomplete="new-password" maxlength="72"></label>
      <button class="primary">Lưu</button>
      <div class="errors" id="pw-err"></div>
    </form>
    <p class="muted small">Đổi xong, các thiết bị khác đang đăng nhập sẽ phải đăng nhập lại.</p></div>`);
  const f = $('#pw');
  f.onsubmit = (e) => {
    e.preventDefault();
    if (f.new1.value !== f.new2.value) { $('#pw-err').textContent = 'Hai mật khẩu mới không khớp'; return; }
    busy(f.querySelector('button'), async () => {
      try {
        await put('/me/password', { current_password: f.cur.value, new_password: f.new1.value });
        toast('Đã đổi mật khẩu. Vui lòng đăng nhập lại.', 'ok');
        lastUser = null;
        await logout();
        location.hash = '#/';
        route();
      } catch (err) { $('#pw-err').textContent = errText(err); }
    });
  };
}

let lastUser = null;
onAuthChange(() => {
  // Session expired (refresh rejected) → back to the login page.
  if (lastUser && !auth.user) { toast('Phiên đăng nhập đã hết, vui lòng đăng nhập lại', 'error'); route(); }
  lastUser = auth.user;
});

window.addEventListener('hashchange', route);
(async () => {
  await resume();
  route();
})();
