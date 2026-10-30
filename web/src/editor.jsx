import React, { useRef, useState } from "react";
const tokens =
  /(R"[^\s()\\]{0,16}\([\s\S]*?\)[^\s()\\]{0,16}"|\/\*[\s\S]*?\*\/|\/\/[^\n]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|^[ \t]*#[^\n]*|\b(?:alignas|alignof|auto|bool|break|case|catch|char|char16_t|char32_t|class|const|constexpr|continue|decltype|default|delete|do|double|else|enum|explicit|extern|false|float|for|friend|if|inline|int|long|mutable|namespace|new|noexcept|nullptr|operator|private|protected|public|register|return|short|signed|sizeof|static|static_assert|struct|switch|template|this|thread_local|throw|true|try|typedef|typename|union|unsigned|using|virtual|void|volatile|wchar_t|while)\b|\b(?:0[xX][\da-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)[uUlLfF]*\b)/gm;
function highlight(value) {
  const out = [];
  let start = 0;
  for (const m of value.matchAll(tokens)) {
    if (m.index > start) out.push(value.slice(start, m.index));
    const s = m[0],
      type =
        s.startsWith("//") || s.startsWith("/*")
          ? "comment"
          : /^[ \t]*#/.test(s)
            ? "preprocessor"
            : /^(?:["']|R")/.test(s)
              ? "string"
              : /^\d/.test(s)
                ? "number"
                : "keyword";
    out.push(
      <span key={m.index} className={`syntax-${type}`}>
        {s}
      </span>,
    );
    start = m.index + s.length;
  }
  out.push(value.slice(start) + "\n");
  return out;
}
export function CodeEditor({ value, onChange, fontSize }) {
  const textarea = useRef(null),
    gutter = useRef(null),
    layer = useRef(null);
  const [cursor, setCursor] = useState([1, 1]),
    [scroll, setScroll] = useState(0);
  const position = () => {
    const t = textarea.current,
      before = t.value.slice(0, t.selectionStart).split("\n");
    setCursor([before.length, before.at(-1).length + 1]);
  };
  const key = (e) => {
    if (e.key === "Escape") {
      e.currentTarget
        .closest(".editor-panel")
        ?.querySelector("button")
        ?.focus();
      return;
    }
    if (e.key !== "Tab") return;
    e.preventDefault();
    const t = e.currentTarget,
      { selectionStart: a, selectionEnd: b, value: v } = t;
    if (!e.shiftKey && a === b) t.setRangeText("    ", a, b, "end");
    else {
      const start = v.lastIndexOf("\n", a - 1) + 1,
        block = v.slice(start, b);
      t.setRangeText(
        e.shiftKey
          ? block.replace(/^ {1,4}/gm, "")
          : block.replace(/^/gm, "    "),
        start,
        b,
        "select",
      );
    }
    onChange(t.value);
    position();
  };
  return (
    <div
      className="code-editor-wrap"
      style={{
        "--editor-font-size": `${fontSize}px`,
        "--editor-line-height": `${Math.round(fontSize * 1.7)}px`,
      }}
    >
      <span className="sr-only" id="editor-help">
        Tab để thụt dòng, Shift + Tab để giảm thụt dòng. Escape chuyển đến thanh
        công cụ. Ctrl hoặc Command + Enter để nộp bài.
      </span>
      <div className="code-editor">
        <div className="gutter" id="gutter" ref={gutter} aria-hidden="true">
          {Array.from({ length: value.split("\n").length }, (_, i) => (
            <div key={i} className={cursor[0] === i + 1 ? "current-line" : ""}>
              {i + 1}
            </div>
          ))}
        </div>
        <div className="code-area">
          <div
            className="active-editor-line"
            style={{
              top: 18 + (cursor[0] - 1) * Math.round(fontSize * 1.7) - scroll,
            }}
          />
          <pre className="syntax-layer" ref={layer} aria-hidden="true">
            {highlight(value)}
          </pre>
          <textarea
            ref={textarea}
            id="code"
            className="code"
            aria-label="Mã nguồn C++17"
            aria-describedby="editor-help"
            value={value}
            onChange={(e) => {
              onChange(e.target.value);
              position();
            }}
            onKeyDown={key}
            onSelect={position}
            onScroll={(e) => {
              layer.current.scrollTop = e.target.scrollTop;
              layer.current.scrollLeft = e.target.scrollLeft;
              gutter.current.scrollTop = e.target.scrollTop;
              setScroll(e.target.scrollTop);
            }}
            spellCheck="false"
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            placeholder={
              "// Viết lời giải C++17 của bạn tại đây.\n// Hoặc tải lên một file .cpp từ thanh công cụ."
            }
          />
        </div>
      </div>
      <div className="cursor-position mono">
        Ln {cursor[0]}, Col {cursor[1]}
      </div>
    </div>
  );
}
