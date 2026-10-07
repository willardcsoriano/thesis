// Local server: the page from public/ and the API from the same app Vercel
// runs. Reads ANTHROPIC_API_KEY and ACCESS_CODE from .env.local (git-ignored).
//
//   npm run dev   -> http://localhost:8787

import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { getRequestListener } from "@hono/node-server";
import { app } from "../src/server.js";

const PORT = Number(process.env.PORT ?? 8787);
const publicDir = fileURLToPath(new URL("../public/", import.meta.url));
const TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".woff2": "font/woff2",
  ".txt": "text/plain; charset=utf-8",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".webmanifest": "application/manifest+json",
};

const api = getRequestListener((request) => app.fetch(request, process.env));

createServer(async (req, res) => {
  const path = new URL(req.url ?? "/", "http://localhost").pathname;
  if (path.startsWith("/api/")) return api(req, res);

  // Resolve inside public/ only; normalize() collapses any "../".
  const file = normalize(join(publicDir, path === "/" ? "index.html" : path));
  if (!file.startsWith(publicDir)) {
    res.writeHead(403).end();
    return;
  }
  try {
    const body = await readFile(file);
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" }).end(body);
  } catch {
    res.writeHead(404).end("Not found");
  }
}).listen(PORT, () => {
  const configured = process.env.ANTHROPIC_API_KEY && process.env.ACCESS_CODE;
  console.log(`SynapseBot at http://localhost:${PORT}${configured ? "" : " (set ANTHROPIC_API_KEY and ACCESS_CODE in .env.local to ask questions)"}`);
});
