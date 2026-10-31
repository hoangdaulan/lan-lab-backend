// Watch the React bundle and serve local assets; forward real REST/WS requests.
import { context } from "esbuild";
import { options } from "./build-ui.mjs";
import { createServer, request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { readFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../web/static", import.meta.url)),
  upstream = new URL(process.env.BACKEND_URL || "http://127.0.0.1:8080");
const forward = upstream.protocol === "https:" ? httpsRequest : httpRequest;
const ctx = await context(options);
await ctx.rebuild();
await ctx.watch();
const server = createServer(async (req, res) => {
  if (req.url.startsWith("/api/")) {
    const proxy = forward(
      new URL(req.url, upstream),
      { method: req.method, headers: req.headers },
      (incoming) => {
        res.writeHead(incoming.statusCode, incoming.headers);
        incoming.pipe(res);
      },
    );
    proxy.on("error", () => {
      res.writeHead(502, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          error:
            "Không kết nối được backend. Hãy khởi động API trên " +
            upstream.origin,
        }),
      );
    });
    req.pipe(proxy);
    return;
  }
  try {
    const path = new URL(req.url, "http://localhost").pathname,
      file = resolve(
        root,
        path === "/" ? "index.html" : path.replace(/^\/assets\//, ""),
      );
    if (!file.startsWith(root + "/")) {
      res.writeHead(404);
      res.end();
      return;
    }
    const content = await readFile(file);
    res.writeHead(200, {
      "Cache-Control": "no-store",
      "Content-Type":
        {
          ".html": "text/html",
          ".js": "text/javascript",
          ".css": "text/css",
          ".woff2": "font/woff2",
          ".svg": "image/svg+xml",
        }[extname(file)] || "application/octet-stream",
    });
    res.end(content);
  } catch {
    res.writeHead(404);
    res.end();
  }
});
server.on("upgrade", (req, socket, head) => {
  if (!req.url.startsWith("/api/")) {
    socket.destroy();
    return;
  }
  const proxy = forward(new URL(req.url, upstream), { headers: req.headers });
  proxy.on("upgrade", (res, target, targetHead) => {
    socket.write(
      `HTTP/1.1 ${res.statusCode} ${res.statusMessage}\r\n` +
        Object.entries(res.headers)
          .map(([k, v]) => `${k}: ${v}`)
          .join("\r\n") +
        "\r\n\r\n",
    );
    if (targetHead.length) socket.write(targetHead);
    if (head.length) target.write(head);
    socket.pipe(target);
    target.pipe(socket);
    socket.on("error", () => target.destroy());
    target.on("error", () => socket.destroy());
  });
  proxy.on("error", () => socket.destroy());
  proxy.end();
});
server.listen(Number(process.env.PORT || 5173), "127.0.0.1", () =>
  console.log(
    `Local: http://127.0.0.1:${server.address().port}\nReal API: ${upstream.origin}\nReact sources are watched; reload the browser after changes.`,
  ),
);
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, async () => {
    await ctx.dispose();
    server.close();
    process.exit(0);
  });
