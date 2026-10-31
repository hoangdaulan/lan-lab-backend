import { useCallback, useEffect, useRef, useState } from "react";
export function useStored(key, fallback) {
  const [value, setValue] = useState(() => {
    try {
      const data = localStorage.getItem(key);
      return data === null ? fallback : JSON.parse(data);
    } catch {
      return fallback;
    }
  });
  const save = useCallback(
    (next) =>
      setValue((old) => {
        const v = typeof next === "function" ? next(old) : next;
        try {
          localStorage.setItem(key, JSON.stringify(v));
        } catch {}
        return v;
      }),
    [key],
  );
  return [value, save];
}
export function useNow(offset = 0) {
  const [now, setNow] = useState(Date.now() + offset);
  useEffect(() => {
    setNow(Date.now() + offset);
    const t = setInterval(() => setNow(Date.now() + offset), 1000);
    return () => clearInterval(t);
  }, [offset]);
  return now;
}
export function useDragResize(ref, orientation, setValue, min, max) {
  const cleanup = useRef(() => {});
  useEffect(() => () => cleanup.current(), []);
  return (event) => {
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const move = (e) => {
      const rect = ref.current?.getBoundingClientRect();
      if (!rect) return;
      const raw =
        orientation === "vertical"
          ? ((e.clientX - rect.left) / rect.width) * 100
          : ((rect.bottom - e.clientY) / rect.height) * 100;
      setValue(Math.max(min, Math.min(max, raw)));
    };
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
    };
    cleanup.current();
    cleanup.current = end;
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  };
}
