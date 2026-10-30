import React, { useCallback, useEffect, useState } from "react";
import { get, post } from "../static/api.js";
import {
  errText,
  toast,
  fmtTime,
  fmtClock,
  fmtDuration,
} from "../static/ui.js";
import { Icon, Loading, Empty, ErrorState } from "./components.jsx";
import { useNow } from "./hooks.js";
export const STATES = {
  running: "Đang diễn ra",
  upcoming: "Sắp diễn ra",
  ended: "Đã kết thúc",
};
export function Sessions() {
  const [sessions, setSessions] = useState(null),
    [error, setError] = useState(null),
    [offset, setOffset] = useState(0);
  const [query, setQuery] = useState(""),
    [filter, setFilter] = useState("all"),
    [joining, setJoining] = useState(null);
  const now = useNow(offset);
  const load = useCallback(async () => {
    try {
      const list = await get("/me/sessions", { pageSize: 200 });
      if (list.server_time)
        setOffset(Date.parse(list.server_time) - Date.now());
      const rank = { running: 0, upcoming: 1, ended: 2 };
      setSessions(
        list.data.sort(
          (a, b) =>
            rank[a.state] - rank[b.state] ||
            (a.state === "ended"
              ? Date.parse(b.starts_at) - Date.parse(a.starts_at)
              : Date.parse(a.starts_at) - Date.parse(b.starts_at)),
        ),
      );
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, []);
  useEffect(() => {
    load();
    const timer = setInterval(load, 30000);
    return () => clearInterval(timer);
  }, [load]);
  // Refresh at a session boundary instead of leaving a stale Join action.
  const boundary = sessions?.some(
    (s) =>
      (s.state === "upcoming" && now >= Date.parse(s.starts_at)) ||
      (s.state === "running" && now >= Date.parse(s.ends_at)),
  );
  useEffect(() => {
    if (boundary) load();
  }, [boundary, load]);
  const join = async (s) => {
    setJoining(s.id);
    try {
      await post(`/sessions/${s.id}/join`);
      location.hash = `#/sessions/${s.id}`;
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      setJoining(null);
    }
  };
  const visible = sessions?.filter(
    (s) =>
      (filter === "all" || s.state === filter) &&
      `${s.exam_title} ${s.name}`
        .toLocaleLowerCase("vi")
        .includes(query.toLocaleLowerCase("vi")),
  );
  return (
    <div className="sessions-page">
      <div className="page-heading">
        <div>
          <span className="breadcrumb">Không gian lập trình / Phiên thi</span>
          <h1>Phiên thi</h1>
          <p>Chọn phiên thi, mở bài tập và bắt đầu làm bài.</p>
        </div>
        <span className="page-meta">
          <span className="status-dot" />
          {sessions ? `${sessions.length} phiên thi` : "Đang tải danh sách"}
        </span>
      </div>
      <div className="browser-toolbar">
        <div className="filter-tabs" aria-label="Lọc trạng thái phiên thi">
          {[
            ["all", "Tất cả"],
            ["running", "Đang diễn ra"],
            ["upcoming", "Sắp diễn ra"],
            ["ended", "Đã kết thúc"],
          ].map(([key, label]) => (
            <button
              key={key}
              className={filter === key ? "active" : ""}
              onClick={() => setFilter(key)}
              aria-pressed={filter === key}
            >
              {label}
              <span className="filter-count">
                {sessions?.filter((s) => key === "all" || s.state === key)
                  .length ?? "–"}
              </span>
            </button>
          ))}
        </div>
        <label className="search-field">
          <Icon name="search" />
          <input
            aria-label="Tìm phiên thi"
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Tìm đề hoặc phiên thi…"
          />
        </label>
      </div>
      <div className="session-scroller">
        {error ? (
          <ErrorState error={{ message: errText(error) }} retry={load} />
        ) : !sessions ? (
          <Loading lines={6} />
        ) : !visible.length ? (
          <Empty
            title={
              query || filter !== "all"
                ? "Không tìm thấy phiên thi"
                : "Chưa có phiên thi"
            }
            icon="book"
          >
            {query || filter !== "all"
              ? "Thử từ khóa hoặc trạng thái khác."
              : "Phiên thi do giáo viên tạo sẽ xuất hiện tại đây."}
          </Empty>
        ) : (
          <table className="session-table">
            <thead>
              <tr>
                <th className="index-column">#</th>
                <th>Đề thi</th>
                <th>Phiên thi</th>
                <th>Thời gian</th>
                <th>Trạng thái</th>
                <th>Thời gian còn lại</th>
                <th aria-label="Thao tác"></th>
              </tr>
            </thead>
            <tbody>
              {visible.map((s, i) => (
                <tr key={s.id}>
                  <td className="index-column mono muted">
                    {String(i + 1).padStart(2, "0")}
                  </td>
                  <td className="session-name">
                    <strong>{s.exam_title}</strong>
                  </td>
                  <td className="session-ca">{s.name}</td>
                  <td className="session-date">
                    <span>{fmtTime(s.starts_at)}</span>
                    <span className="muted">→ {fmtTime(s.ends_at)}</span>
                  </td>
                  <td className="session-state">
                    <span
                      className={`badge ${s.state === "running" ? "ok" : s.state === "upcoming" ? "info" : ""}`}
                    >
                      <span className="status-dot" />
                      {STATES[s.state]}
                    </span>
                    {s.participation && (
                      <span className="participation-note muted">
                        {s.participation.status === "finished"
                          ? "Bạn đã kết thúc"
                          : "Đã tham gia"}
                      </span>
                    )}
                  </td>
                  <td className="session-time mono">
                    {s.state === "ended" ? (
                      "—"
                    ) : (
                      <>
                        <Icon name="clock" size={14} />
                        {fmtDuration(
                          Date.parse(
                            s.state === "upcoming" ? s.starts_at : s.ends_at,
                          ) - now,
                        )}
                      </>
                    )}
                  </td>
                  <td className="session-action">
                    {s.participation ? (
                      <a
                        className={`btn ${s.state === "running" && s.participation.status !== "finished" ? "primary" : ""}`}
                        href={`#/sessions/${s.id}`}
                      >
                        {s.state === "running" &&
                        s.participation.status !== "finished"
                          ? "Tiếp tục làm bài"
                          : "Xem phiên thi"}
                        <Icon name="arrow" size={14} />
                      </a>
                    ) : s.state === "running" ? (
                      <button
                        className="primary"
                        disabled={joining === s.id}
                        onClick={() => join(s)}
                      >
                        {joining === s.id ? "Đang tham gia…" : "Vào làm bài"}
                        <Icon name="arrow" size={14} />
                      </button>
                    ) : (
                      <span className="muted">
                        {s.state === "upcoming"
                          ? "Chưa mở tham gia"
                          : "Đã đóng"}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <div className="list-footnote">
        <span>
          <Icon name="info" size={14} />
          Thời gian đồng bộ theo máy chủ. Danh sách tự cập nhật.
        </span>
        <span>C++17 · Một file .cpp · Tối đa 64 KB</span>
      </div>
    </div>
  );
}
