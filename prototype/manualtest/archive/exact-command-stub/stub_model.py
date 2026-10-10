#!/usr/bin/env python3
"""A stand-in for the Ollama model, for manual tests that need an exact command.

Serves the two endpoints synapse uses: GET /api/tags (its health check) and
POST /api/generate. The first step request is answered with the command given on the
command line, every later one with "done", and the end-of-task summary request with a
fixed sentence. Everything downstream of the model (the safety gate, backups,
execution, the undo journal) is the real product.

Usage: stub_model.py PORT_FILE COMMAND   (writes the port it listens on to PORT_FILE)
Stdlib only.
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port_file, command = sys.argv[1], sys.argv[2]
steps = 0


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.reply({"models": [{"name": "stub"}]})

    def do_POST(self):
        global steps
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if b"You are reporting the outcome of a task" in body:
            text = "The command has finished."
        else:
            steps += 1
            decision = {"action": "run", "command": command} if steps == 1 else {"action": "done", "command": ""}
            text = json.dumps(decision)
        self.reply({"model": "stub", "response": text, "done": True, "eval_count": 1})


server = HTTPServer(("127.0.0.1", 0), Handler)
with open(port_file, "w") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
