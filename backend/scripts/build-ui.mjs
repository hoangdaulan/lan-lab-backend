import { build, context } from "esbuild";
import { fileURLToPath } from "node:url";
export const options = {
  absWorkingDir: fileURLToPath(new URL("../../", import.meta.url)),
  entryPoints: ["web/src/app.jsx"],
  outfile: "backend/web/static/app.js",
  bundle: true,
  format: "esm",
  target: ["es2022"],
  minify: true,
  legalComments: "eof",
  define: { "process.env.NODE_ENV": '"production"' },
  plugins: [
    {
      name: "shared-api-modules",
      setup(builder) {
        builder.onResolve({ filter: /^\.\.\/static\/.*\.js$/ }, ({ path }) => ({
          path: "/assets/" + path.split("/").at(-1),
          external: true,
        }));
      },
    },
  ],
};
if (import.meta.url === new URL(process.argv[1], "file:").href) {
  if (process.argv.includes("--watch")) {
    const ctx = await context(options);
    await ctx.watch();
    console.log("Watching React sources.");
  } else {
    await build(options);
    console.log("Built backend/web/static/app.js — local assets, no CDN.");
  }
}
