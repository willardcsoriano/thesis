// Vercel Function serving every /api/* route.
//
// The Node runtime calls a default export with Node's (IncomingMessage,
// ServerResponse). getRequestListener bridges those to the app's Web
// Request/Response, streaming included; askwillard runs the same bridge in
// production.

import { getRequestListener } from "@hono/node-server";
import { app } from "../src/server.js";

export default getRequestListener((request) => app.fetch(request, process.env));
