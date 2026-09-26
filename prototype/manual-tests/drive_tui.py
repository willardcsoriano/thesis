#!/usr/bin/env python3
"""Drives a terminal program (the SynapseOS TUI) under a real pty, scripted and
non-interactive: sends a fixed sequence of keystrokes with waits between them,
records everything the program writes back, and exits on its own. This is
automated CLI testing, the same principle as the project's own tui_test.go
(driving a real input/output pair and inspecting what comes back), just at the
process boundary instead of the Go API — not a live, human-interactive session.

Usage: python3 drive_tui.py <cmd...> -- <keystroke>:<wait_seconds> ...
A keystroke of "CTRLC" sends ETX (0x03). Output is written to stdout at the end,
both raw (with ANSI escapes) and a stripped plain-text version.
"""
import fcntl
import os
import pty
import re
import select
import struct
import subprocess
import sys
import termios
import time

ANSI = re.compile(rb"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][AB012]|\r")


def strip_ansi(b: bytes) -> str:
    return ANSI.sub(b"", b).decode("utf-8", "replace")


def main():
    sep = sys.argv.index("--")
    cmd = sys.argv[1:sep]
    steps = sys.argv[sep + 1:]

    master, slave = pty.openpty()
    # A pty with no window size reported (0x0) makes many TUI frameworks stall or
    # refuse to render at all. Set a real size before the child starts.
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
    env = dict(os.environ)
    env.setdefault("TERM", "xterm-256color")
    proc = subprocess.Popen(
        cmd, stdin=slave, stdout=slave, stderr=slave,
        preexec_fn=os.setsid, close_fds=True, env=env,
    )
    os.close(slave)

    buf = bytearray()

    def drain(timeout):
        end = time.time() + timeout
        while time.time() < end:
            r, _, _ = select.select([master], [], [], max(0, end - time.time()))
            if not r:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError:
                return
            if not chunk:
                return
            buf.extend(chunk)

    drain(1.0)  # initial paint
    for step in steps:
        key, _, wait = step.partition(":")
        wait = float(wait) if wait else 1.0
        # Raw terminal mode: Enter arrives as CR (\r), not LF. A literal "\n" in a
        # step means "press Enter".
        data = b"\x03" if key == "CTRLC" else (key.encode().replace(b"\\n", b"\r").replace(b"\\r", b"\r"))
        try:
            os.write(master, data)
        except OSError:
            break
        drain(wait)

    if proc.poll() is None:
        proc.terminate()
        time.sleep(0.5)
        if proc.poll() is None:
            proc.kill()
    drain(0.5)
    os.close(master)

    if len(sys.argv) > 1 and os.environ.get("DUMP_RAW"):
        with open(os.environ["DUMP_RAW"], "wb") as f:
            f.write(bytes(buf))
    sys.stderr.write(f"--- exit code: {proc.poll()} ---\n")
    sys.stdout.write("=== PLAIN (ANSI-stripped) ===\n")
    sys.stdout.write(strip_ansi(bytes(buf)))
    sys.stdout.write("\n=== RAW BYTE COUNT ===\n")
    sys.stdout.write(f"{len(buf)} bytes captured\n")


if __name__ == "__main__":
    main()
