"""Authenticated localhost bridge between PAL CTRL and native Palworld on Windows."""

from __future__ import annotations

import base64
import hmac
import json
import os
import subprocess
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse


ROOT = Path(__file__).resolve().parent
SERVER_DIR = Path(os.getenv("PALWORLD_SERVER_DIR", r"D:\PalworldServer\server"))
STEAMCMD = Path(
    os.getenv("PALWORLD_STEAMCMD", r"D:\PalworldServer\steamcmd\steamcmd.exe")
)
LAUNCHER = Path(
    os.getenv(
        "PALWORLD_LAUNCHER",
        str(ROOT / "scripts" / "Start-PalworldLimited.ps1"),
    )
)
RUNTIME_DIR = Path(os.getenv("PALWORLD_RUNTIME_DIR", r"D:\PalworldServer\logs"))
CONTROL_TOKEN = os.getenv("PALWORLD_CONTROL_TOKEN", "")
ADMIN_PASSWORD = os.getenv("PALWORLD_ADMIN_PASSWORD", "")
MAX_MEMORY_GB = int(os.getenv("PALWORLD_MAX_MEMORY_GB", "8"))
MAX_PLAYERS = int(os.getenv("PALWORLD_MAX_PLAYERS", "4"))
LISTEN_ADDR = os.getenv("PALWORLD_AGENT_ADDR", "0.0.0.0")
LISTEN_PORT = int(os.getenv("PALWORLD_AGENT_PORT", "8213"))
POWERSHELL = (
    Path(os.environ["SystemRoot"])
    / "System32"
    / "WindowsPowerShell"
    / "v1.0"
    / "powershell.exe"
)

_action_lock = threading.Lock()
_cpu_lock = threading.Lock()
_last_cpu_seconds: float | None = None
_last_cpu_time: float | None = None


def powershell_json(script: str) -> object:
    completed = subprocess.run(
        [str(POWERSHELL), "-NoProfile", "-Command", script],
        check=True,
        capture_output=True,
        text=True,
        timeout=15,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    output = completed.stdout.strip()
    return json.loads(output) if output else []


def process_snapshot() -> dict[str, object]:
    processes = powershell_json(
        "$p=@(Get-Process -Name 'PalServer','PalServer-Win64-Shipping-Cmd' "
        "-ErrorAction SilentlyContinue); "
        "@($p | Select-Object Id,ProcessName,WorkingSet64,PrivateMemorySize64,"
        "CPU,StartTime) | ConvertTo-Json -Compress"
    )
    if isinstance(processes, dict):
        processes = [processes]

    working_set = sum(int(item.get("WorkingSet64") or 0) for item in processes)
    private_bytes = sum(int(item.get("PrivateMemorySize64") or 0) for item in processes)
    cpu_seconds = sum(float(item.get("CPU") or 0) for item in processes)
    now = time.monotonic()

    global _last_cpu_seconds, _last_cpu_time
    with _cpu_lock:
        cpu_percent = 0.0
        if _last_cpu_seconds is not None and _last_cpu_time is not None:
            elapsed = now - _last_cpu_time
            if elapsed > 0:
                cpu_percent = (
                    max(0.0, cpu_seconds - _last_cpu_seconds)
                    / elapsed
                    / max(1, os.cpu_count() or 1)
                    * 100
                )
        _last_cpu_seconds = cpu_seconds
        _last_cpu_time = now

    limit_bytes = MAX_MEMORY_GB * 1024**3
    memory_percent = working_set / limit_bytes * 100 if limit_bytes else 0.0
    running = bool(processes)
    return {
        "status": "running" if running else "stopped",
        "stats": {
            "cpuPercent": f"{cpu_percent:.1f}%",
            "memoryUsage": (
                f"{working_set / 1024**3:.2f}GiB / {MAX_MEMORY_GB}GiB"
            ),
            "memoryPercent": f"{memory_percent:.1f}%",
            "networkIO": "native Windows",
            "blockIO": f"{private_bytes / 1024**3:.2f}GiB private",
        },
    }


def is_running() -> bool:
    return process_snapshot()["status"] == "running"


def palworld_request(path: str, method: str = "POST", body: dict | None = None) -> bytes:
    credentials = base64.b64encode(f"admin:{ADMIN_PASSWORD}".encode()).decode()
    data = json.dumps(body).encode() if body is not None else b""
    request = urllib.request.Request(
        "http://127.0.0.1:8212/v1/api" + path,
        data=data,
        method=method,
        headers={
            "Authorization": "Basic " + credentials,
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        return response.read()


def start_server() -> str:
    if is_running():
        return "Palworld is already running"
    if not LAUNCHER.is_file():
        raise RuntimeError(f"launcher not found: {LAUNCHER}")

    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    output = open(RUNTIME_DIR / "palworld-runtime.log", "a", encoding="utf-8")
    error = open(RUNTIME_DIR / "palworld-runtime-error.log", "a", encoding="utf-8")
    subprocess.Popen(
        [
            str(POWERSHELL),
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(LAUNCHER),
            "-ServerDir",
            str(SERVER_DIR),
            "-MaxMemoryGB",
            str(MAX_MEMORY_GB),
            "-Players",
            str(MAX_PLAYERS),
            "-Port",
            "8211",
        ],
        cwd=str(ROOT),
        stdin=subprocess.DEVNULL,
        stdout=output,
        stderr=error,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    deadline = time.monotonic() + 45
    process_seen = False
    while time.monotonic() < deadline:
        running = is_running()
        if running:
            process_seen = True
        elif process_seen:
            raise RuntimeError("Palworld exited during startup")
        if running:
            try:
                palworld_request("/info", method="GET")
                return f"Palworld started with {MAX_MEMORY_GB} GB cap"
            except (OSError, urllib.error.URLError):
                pass
        time.sleep(0.5)
    raise RuntimeError("Palworld REST API did not become ready within 45 seconds")


def stop_server() -> str:
    if not is_running():
        return "Palworld is already stopped"
    palworld_request("/stop")
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if not is_running():
            return "Palworld stopped"
        time.sleep(0.5)
    raise RuntimeError("Palworld did not stop within 30 seconds")


def restart_server() -> str:
    if is_running():
        palworld_request(
            "/shutdown",
            body={"waittime": 1, "message": "Server restarting from PAL CTRL"},
        )
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline and is_running():
            time.sleep(0.5)
        if is_running():
            raise RuntimeError("Palworld did not stop for restart")
    start_server()
    return f"Palworld restarted with {MAX_MEMORY_GB} GB cap"


def update_server() -> str:
    if is_running():
        raise RuntimeError("Stop Palworld before updating")
    completed = subprocess.run(
        [
            str(STEAMCMD),
            "+force_install_dir",
            str(SERVER_DIR),
            "+login",
            "anonymous",
            "+app_update",
            "2394010",
            "validate",
            "+quit",
        ],
        check=False,
        capture_output=True,
        text=True,
        timeout=600,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    if completed.returncode != 0 or "fully installed" not in completed.stdout:
        detail = (completed.stderr or completed.stdout)[-1000:].strip()
        raise RuntimeError("SteamCMD update failed: " + detail)
    return "Palworld updated and validated"


def log_lines(tail: int) -> list[str]:
    candidates = list((SERVER_DIR / "Pal" / "Saved" / "Logs").glob("*.log"))
    candidates.extend(
        [
            RUNTIME_DIR / "palworld-runtime.log",
            RUNTIME_DIR / "palworld-runtime-error.log",
        ]
    )
    candidates = [path for path in candidates if path.is_file()]
    if not candidates:
        return []
    newest = max(candidates, key=lambda path: path.stat().st_mtime)
    text = newest.read_text(encoding="utf-8", errors="replace")
    return text.splitlines()[-tail:]


class Handler(BaseHTTPRequestHandler):
    server_version = "PalworldNativeAgent/1.0"

    def authorized(self) -> bool:
        provided = self.headers.get("X-Pal-Control-Token", "")
        return bool(CONTROL_TOKEN) and hmac.compare_digest(provided, CONTROL_TOKEN)

    def send_json(self, status: int, payload: dict[str, object]) -> None:
        encoded = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self) -> None:
        if not self.authorized():
            self.send_json(401, {"error": "unauthorized"})
            return
        parsed = urlparse(self.path)
        try:
            if parsed.path == "/status":
                self.send_json(200, process_snapshot())
                return
            if parsed.path == "/logs":
                requested = int(parse_qs(parsed.query).get("tail", ["240"])[0])
                tail = min(1000, max(1, requested))
                self.send_json(200, {"lines": log_lines(tail)})
                return
            self.send_json(404, {"error": "not found"})
        except Exception as error:
            self.send_json(500, {"error": str(error)})

    def do_POST(self) -> None:
        if not self.authorized():
            self.send_json(401, {"error": "unauthorized"})
            return
        actions = {
            "/start": start_server,
            "/stop": stop_server,
            "/restart": restart_server,
            "/update": update_server,
        }
        action = actions.get(urlparse(self.path).path)
        if action is None:
            self.send_json(404, {"error": "not found"})
            return
        try:
            with _action_lock:
                message = action()
            self.send_json(200, {"message": message})
        except (RuntimeError, OSError, subprocess.SubprocessError, urllib.error.URLError) as error:
            self.send_json(502, {"error": str(error)})

    def log_message(self, format: str, *args: object) -> None:
        print(
            f"{time.strftime('%Y-%m-%dT%H:%M:%S')} "
            f"{self.client_address[0]} {format % args}",
            flush=True,
        )


def main() -> None:
    if not CONTROL_TOKEN:
        raise SystemExit("PALWORLD_CONTROL_TOKEN is required")
    if not ADMIN_PASSWORD:
        raise SystemExit("PALWORLD_ADMIN_PASSWORD is required")
    print(
        f"Native control agent listening on {LISTEN_ADDR}:{LISTEN_PORT}; "
        f"Palworld cap={MAX_MEMORY_GB}GB players={MAX_PLAYERS}",
        flush=True,
    )
    ThreadingHTTPServer((LISTEN_ADDR, LISTEN_PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
