import React from "react";
const shapes = {
  code: "m8 6-6 6 6 6m8-12 6 6-6 6m-2-16-4 20",
  arrow: "M4 12h16m-6-6 6 6-6 6",
  back: "M20 12H4m6-6-6 6 6 6",
  down: "m6 9 6 6 6-6",
  right: "m9 5 7 7-7 7",
  search: "m21 21-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
  book: "M4 3h16v18H6a3 3 0 0 1-3-3V5a2 2 0 0 1 2-2m-2 15a3 3 0 0 1 3-3h14M8 7h8",
  history: "M3 10a9 9 0 1 1 2 8M3 3v7h7m2-4v7l4 2",
  clock: "M12 7v6l3 2M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
  layers: "m12 3 10 5-10 5L2 8l10-5M2 12l10 5 10-5M2 16l10 5 10-5",
  terminal: "m4 6 6 6-6 6m9 0h7",
  check: "m5 12 4 4L19 6",
  x: "m6 6 12 12M6 18 18 6",
  upload: "M12 16V3m-5 5 5-5 5 5M4 15v6h16v-6",
  download: "M12 3v13m-5-5 5 5 5-5M4 16v5h16v-5",
  expand: "M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5",
  collapse: "M3 8h5V3m13 5h-5V3M8 21v-5H3m13 5v-5h5",
  sun: "M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1M17 12a5 5 0 1 1-10 0 5 5 0 0 1 10 0",
  moon: "M21 13a9 9 0 1 1-10-10 7 7 0 0 0 10 10",
  logout: "M9 4H3v16h6m5-12 4 4-4 4m-6-4h13",
  monitor: "M3 3h18v13H3V3m5 18h8m-4-5v5",
  info: "M12 11v6m0-10v.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
  alert: "M12 8v5m0 3v.01M12 2 1 21h22L12 2",
  settings: "M4 6h16M4 12h16M4 18h16M8 3v6m8 0v6m-6 0v6",
  help: "M9 9a3 3 0 1 1 5 2c-2 1-2 2-2 3m0 3v.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
  file: "M14 2H5v20h14V7l-5-5v6h5M8 12h8m-8 4h6",
  users:
    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2m20 0v-2a4 4 0 0 0-3-4M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0m5-4a4 4 0 0 1 0 8",
};
export function Icon({ name, size = 16, ...props }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.65"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      <path d={shapes[name] || shapes.code} />
    </svg>
  );
}
export function Brand() {
  return (
    <a className="brand" href="#/" aria-label="LAN Judge — Trang chủ">
      <span className="brand-symbol">
        <Icon name="code" size={21} />
      </span>
      <span>
        LAN<span className="brand-slash">/</span>
        <strong>JUDGE</strong>
      </span>
    </a>
  );
}
export function Loading({ lines = 4 }) {
  return (
    <div className="skeleton" role="status" aria-label="Đang tải dữ liệu">
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} style={{ width: `${86 - (i % 3) * 13}%` }} />
      ))}
    </div>
  );
}
export function Empty({ title, children, icon = "file" }) {
  return (
    <div className="empty-state">
      <Icon name={icon} size={27} />
      <h3>{title}</h3>
      {children && <p>{children}</p>}
    </div>
  );
}
export function ErrorState({ error, retry }) {
  return (
    <div className="error-state" role="alert">
      <Icon name="alert" size={19} />
      <div>
        <strong>Không tải được dữ liệu</strong>
        <p>{error?.message || String(error)}</p>
        {retry && <button onClick={retry}>Thử lại</button>}
      </div>
    </div>
  );
}
export const VERDICTS = {
  AC: ["Accepted", "Tất cả bộ test đều vượt qua.", "ok"],
  WA: [
    "Wrong Answer",
    "Kết quả chưa khớp với đáp án. Hãy kiểm tra lại lời giải.",
    "bad",
  ],
  CE: [
    "Compilation Error",
    "Chương trình chưa biên dịch được. Xem thông báo bên dưới.",
    "bad",
  ],
  RE: ["Runtime Error", "Chương trình gặp lỗi trong khi thực thi.", "bad"],
  TLE: ["Time Limit Exceeded", "Chương trình vượt giới hạn thời gian.", "wait"],
  MLE: ["Memory Limit Exceeded", "Chương trình vượt giới hạn bộ nhớ.", "wait"],
};
export function ResultBadge({ result: r }) {
  if (!r) return <span className="badge">Chờ chấm lại</span>;
  if (r.status === "system_error")
    return <span className="badge bad">Lỗi hệ thống</span>;
  if (r.status !== "completed")
    return (
      <span className="badge wait">
        <span className="spinner" />
        {r.status === "judging" ? "Đang chấm" : "Đang chờ"}
      </span>
    );
  return (
    <span
      className={`badge ${VERDICTS[r.verdict]?.[2] || "bad"}`}
      title={VERDICTS[r.verdict]?.[1]}
    >
      <Icon name={r.verdict === "AC" ? "check" : "x"} size={13} />
      {VERDICTS[r.verdict]?.[0] || r.verdict}
    </span>
  );
}
export function tabKeys(e) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
  e.preventDefault();
  const items = [...e.currentTarget.querySelectorAll('[role="tab"]')];
  const next =
    e.key === "Home"
      ? items[0]
      : e.key === "End"
        ? items.at(-1)
        : items[
            (items.indexOf(document.activeElement) +
              (e.key === "ArrowRight" ? 1 : -1) +
              items.length) %
              items.length
          ];
  next?.focus();
  next?.click();
}
