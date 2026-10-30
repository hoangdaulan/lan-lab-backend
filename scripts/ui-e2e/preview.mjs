import { startFixtures } from "./fixture-server.mjs";
const server = await startFixtures(Number(process.env.PORT || 5173));
console.log(
  `UI preview: ${server.url}\nTEST FIXTURES ONLY — no C++ execution.\nStudent: SV001 / demo\nTeacher: GV001 / demo\nCtrl+C to stop.`,
);
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, async () => {
    await server.stop();
    process.exit(0);
  });
