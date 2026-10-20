#!/usr/bin/env python3
"""Benchmark ghx CLI invocations against the JSON-RPC server.

The CLI pays process startup, flag parsing, and store initialization on every
call. The RPC server pays those once at startup and then serves requests over a
kept-alive HTTP connection. This script times the same read operations through
both paths against one cache so the difference is visible.

Only read-only commands are executed. By default the benchmark builds a
throwaway cache in a temp directory, so the real ~/.cache/ghx is untouched.

Typical usage::

    python3 scripts/benchmark_rpc.py --build
    python3 scripts/benchmark_rpc.py --repo TomzxCode/ghx --runs 50
    python3 scripts/benchmark_rpc.py --cache-dir ~/.cache/ghx/cache --runs 50
    python3 scripts/benchmark_rpc.py --json results.json -o results.md
"""

from __future__ import annotations

import argparse
import http.client
import json
import math
import os
import platform
import shutil
import socket
import statistics
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
BASE_ENV = {
    "NO_COLOR": "1",
    "CLICOLOR": "0",
    "CLICOLOR_FORCE": "0",
    "TERM": "dumb",
}


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


@dataclass
class Case:
    """One operation reachable through the CLI and, optionally, over RPC."""

    key: str
    label: str
    cli_args: list[str] | None
    method: str | None = None
    params: dict | None = None
    remote_command: list[str] | None = None
    note: str = ""


@dataclass
class Samples:
    durations: list[float] = field(default_factory=list)
    bytes_read: list[int] = field(default_factory=list)
    error: str | None = None

    @property
    def ok(self) -> bool:
        return self.error is None and bool(self.durations)


@dataclass
class Config:
    binary: Path
    repo: str
    cache_dir: Path
    storage: str
    runs: int
    warmup: int
    timeout: float
    url: str
    log_path: Path


# ---------------------------------------------------------------------------
# Statistics and formatting
# ---------------------------------------------------------------------------


def percentile(sorted_vals: list[float], p: float) -> float:
    if not sorted_vals:
        return 0.0
    if len(sorted_vals) == 1:
        return sorted_vals[0]
    k = (len(sorted_vals) - 1) * p / 100.0
    lo, hi = math.floor(k), math.ceil(k)
    if lo == hi:
        return sorted_vals[lo]
    return sorted_vals[lo] + (sorted_vals[hi] - sorted_vals[lo]) * (k - lo)


def summarize(values: list[float]) -> dict[str, float]:
    if not values:
        return {"n": 0, "median": 0.0, "mean": 0.0, "p95": 0.0, "min": 0.0, "max": 0.0, "stdev": 0.0}
    ordered = sorted(values)
    return {
        "n": float(len(ordered)),
        "median": statistics.median(ordered),
        "mean": statistics.fmean(ordered),
        "p95": percentile(ordered, 95),
        "min": ordered[0],
        "max": ordered[-1],
        "stdev": statistics.pstdev(ordered) if len(ordered) > 1 else 0.0,
    }


def fmt_time(seconds: float) -> str:
    ms = seconds * 1000.0
    if ms < 1.0:
        return f"{seconds * 1_000_000:.0f} us"
    if ms < 1000.0:
        return f"{ms:.1f} ms"
    return f"{ms / 1000.0:.2f} s"


def fmt_speedup(ratio: float) -> str:
    if ratio <= 0:
        return "n/a"
    if ratio >= 1.0:
        return f"{ratio:.2f}x"
    return f"{1.0 / ratio:.2f}x slower"


# ---------------------------------------------------------------------------
# Execution
# ---------------------------------------------------------------------------


class RPCError(RuntimeError):
    pass


class RPCClient:
    """Minimal JSON-RPC client over one kept-alive HTTP connection."""

    def __init__(self, url: str) -> None:
        self.host, self.port = parse_url(url)
        self.conn = http.client.HTTPConnection(self.host, self.port, timeout=60)
        self.next_id = 0

    def call(self, method: str, params: dict | None) -> tuple[int, bytes]:
        self.next_id += 1
        body = json.dumps(
            {"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params or {}},
            separators=(",", ":"),
        ).encode()
        self.conn.request(
            "POST",
            "/rpc",
            body=body,
            headers={"Content-Type": "application/json", "Accept": "application/json"},
        )
        response = self.conn.getresponse()
        data = response.read()
        if response.status != 200:
            raise RPCError(f"HTTP {response.status}: {data[:200]!r}")
        message = json.loads(data)
        if message.get("error"):
            raise RPCError(str(message["error"]))
        return response.status, data

    def close(self) -> None:
        self.conn.close()


def parse_url(url: str) -> tuple[str, int]:
    rest = url.split("://", 1)[-1]
    host, _, port = rest.partition(":")
    return host, int(port or "80")


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def base_env() -> dict[str, str]:
    return {**os.environ, **BASE_ENV}


def cli_base(cfg: Config) -> list[str]:
    return [
        str(cfg.binary),
        "--cache-dir",
        str(cfg.cache_dir),
        "--storage",
        cfg.storage,
        "--telemetry=false",
    ]


def run_cli(args: list[str], cfg: Config) -> tuple[float, int]:
    start = time.perf_counter()
    proc = subprocess.run(
        [*cli_base(cfg), *args],
        capture_output=True,
        timeout=cfg.timeout,
        env=base_env(),
        check=False,
    )
    elapsed = time.perf_counter() - start
    if proc.returncode != 0:
        raise RuntimeError(f"exit {proc.returncode}: {proc.stderr.decode(errors='replace')[:300]}")
    return elapsed, len(proc.stdout or b"")


def run_rpc(client: RPCClient, case: Case) -> tuple[float, int]:
    start = time.perf_counter()
    _, data = client.call(case.method or "", case.params)
    elapsed = time.perf_counter() - start
    return elapsed, len(data)


def make_cases(cfg: Config, issue: int | None, pr: int | None) -> list[Case]:
    repo = ["--repo", cfg.repo]
    cases = [
        Case(
            "repo-list",
            "repo list",
            ["repo", "list", *repo],
            "repo.list",
            {},
            note="Both read the same local store; the CLI opens it per call.",
        ),
        Case(
            "issue-list",
            "issue list (all, limit 100)",
            ["issue", "list", "--state", "all", "--limit", "100", "--json", *repo],
            "issue.list",
            {"repo": cfg.repo, "state": "all", "limit": 100},
            note="Fresh cache on both paths, so neither calls GitHub.",
        ),
        Case(
            "pr-list",
            "pr list (all, limit 100)",
            ["pr", "list", "--state", "all", "--limit", "100", "--json", *repo],
            "pr.list",
            {"repo": cfg.repo, "state": "all", "limit": 100},
            note="Fresh cache on both paths, so neither calls GitHub.",
        ),
    ]
    if issue is not None:
        cases.append(
            Case(
                "issue-view",
                f"issue view #{issue}",
                ["issue", "view", str(issue), "--json", *repo],
                "issue.view",
                {"repo": cfg.repo, "number": issue},
                note="Served from the fresh cache; no backfill.",
            )
        )
    if pr is not None:
        cases.append(
            Case(
                "pr-view",
                f"pr view #{pr}",
                ["pr", "view", str(pr), "--json", *repo],
                "pr.view",
                {"repo": cfg.repo, "number": pr},
                note="Served from the fresh cache; no backfill.",
            )
        )
    cases.extend(
        [
            Case(
                "cli-run",
                "cli.run repo list",
                ["repo", "list", *repo],
                "cli.run",
                {"args": ["repo", "list", *repo]},
                note="RPC parity path still spawns the CLI on the server, so it adds HTTP on top.",
            ),
            Case(
                "remote",
                "--remote repo list",
                ["repo", "list", *repo],
                remote_command=["repo", "list", *repo],
                note="Transparent remote mode: client process + HTTP + server-side child process.",
            ),
        ]
    )
    return cases


def benchmark(case: Case, cfg: Config, rpc: RPCClient) -> tuple[Samples, Samples]:
    """Run interleaved warmups and timed samples for one case."""
    cli, http = Samples(), Samples()

    def attempt(slot: Samples, fn) -> None:
        try:
            elapsed, size = fn()
        except Exception as exc:  # noqa: BLE001 - report the failure, do not abort the run
            slot.error = str(exc)
            return
        slot.durations.append(elapsed)
        slot.bytes_read.append(size)

    for i in range(cfg.warmup + cfg.runs):
        timed = i >= cfg.warmup
        order = ("cli", "rpc") if i % 2 == 0 else ("rpc", "cli")
        for kind in order:
            if kind == "cli":
                if case.cli_args is None:
                    continue
                if timed:
                    attempt(cli, lambda: run_cli(case.cli_args, cfg))
            else:
                if case.remote_command is not None:
                    args = ["--remote", cfg.url, *case.remote_command]
                    if timed:
                        attempt(http, lambda: run_cli(args, cfg))
                elif case.method is not None:
                    if timed:
                        attempt(http, lambda: run_rpc(rpc, case))
    return cli, http


# ---------------------------------------------------------------------------
# Server and cache setup
# ---------------------------------------------------------------------------


def build_binary() -> Path:
    out = Path(tempfile.mkdtemp(prefix="ghx-rpc-bench-build-")) / "ghx"
    print("Building ghx...")
    proc = subprocess.run(
        ["go", "build", "-o", str(out), "."],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(f"error: go build failed:\n{proc.stderr.strip()}")
    return out


def resolve_binary(explicit: str | None) -> Path:
    if explicit:
        path = Path(explicit).expanduser()
        if path.exists():
            return path.resolve()
        found = shutil.which(explicit)
        if found:
            return Path(found)
        raise SystemExit(f"error: ghx binary not found at '{explicit}'")
    found = shutil.which("ghx")
    if found:
        return Path(found)
    local = REPO_ROOT / "ghx"
    if local.exists():
        return local
    raise SystemExit("error: 'ghx' not found on PATH; pass --build or --ghx PATH")


def prepare_cache(cfg: Config) -> None:
    print(f"Populating cache at {cfg.cache_dir} for {cfg.repo}...")
    proc = subprocess.run(
        [
            str(cfg.binary),
            "--cache-dir",
            str(cfg.cache_dir),
            "--storage",
            cfg.storage,
            "cache",
            "--repo",
            cfg.repo,
            "--telemetry=false",
        ],
        capture_output=True,
        text=True,
        env=base_env(),
        check=False,
    )
    if proc.returncode != 0:
        raise SystemExit(f"error: populating cache failed:\n{(proc.stderr or proc.stdout).strip()}")


def start_server(cfg: Config, log_file) -> subprocess.Popen:
    proc = subprocess.Popen(
        [
            str(cfg.binary),
            "--cache-dir",
            str(cfg.cache_dir),
            "--storage",
            cfg.storage,
            "rpc",
            "serve",
            "--host",
            "127.0.0.1",
            "--port",
            str(parse_url(cfg.url)[1]),
            "--allow-exec",
            "--telemetry=false",
            "--log-level",
            "error",
        ],
        stdout=log_file,
        stderr=subprocess.STDOUT,
        env=base_env(),
    )
    deadline = time.time() + 15
    while time.time() < deadline:
        if proc.poll() is not None:
            raise SystemExit(f"error: rpc server exited with {proc.returncode}")
        try:
            client = RPCClient(cfg.url)
            client.call("server.info", {})
            client.close()
            return proc
        except (OSError, RPCError, json.JSONDecodeError):
            time.sleep(0.05)
    proc.terminate()
    raise SystemExit("error: rpc server did not become ready")


def discover_numbers(cfg: Config) -> tuple[int | None, int | None]:
    def first_number(args: list[str]) -> int | None:
        proc = subprocess.run(
            [*cli_base(cfg), *args, "--json"],
            capture_output=True,
            text=True,
            env=base_env(),
            check=False,
        )
        if proc.returncode != 0:
            return None
        try:
            items = json.loads(proc.stdout or "[]")
        except json.JSONDecodeError:
            return None
        if not isinstance(items, list) or not items:
            return None
        return int(items[0]["number"])

    issue = first_number(["issue", "list", "--state", "all", "--limit", "1", "--repo", cfg.repo])
    pr = first_number(["pr", "list", "--state", "all", "--limit", "1", "--repo", cfg.repo])
    return issue, pr


# ---------------------------------------------------------------------------
# Reporting
# ---------------------------------------------------------------------------


def render_markdown(results: list[dict], cfg: Config) -> str:
    lines = [
        "# CLI vs JSON-RPC benchmark",
        "",
        f"- Repository: `{cfg.repo}`",
        f"- Runs per path: {cfg.runs} timed + {cfg.warmup} warmup",
        f"- Storage: `{cfg.storage}`",
        f"- Generated: {time.strftime('%Y-%m-%d %H:%M UTC', time.gmtime())}",
        "",
        "| Operation | CLI median | RPC median | CLI p95 | RPC p95 | Speedup |",
        "|---|---:|---:|---:|---:|---:|",
    ]
    for row in results:
        cli = row["cli"]["median"] if row["cli"]["n"] else None
        rpc = row["rpc"]["median"] if row["rpc"]["n"] else None
        speedup = (cli / rpc) if cli and rpc else None
        lines.append(
            f"| {row['label']} | {fmt_time(cli) if cli else 'n/a'} | "
            f"{fmt_time(rpc) if rpc else 'n/a'} | "
            f"{fmt_time(row['cli']['p95']) if cli else 'n/a'} | "
            f"{fmt_time(row['rpc']['p95']) if rpc else 'n/a'} | "
            f"{fmt_speedup(speedup) if speedup else 'n/a'} |"
        )
    lines.append("")
    lines.append("Speedup is CLI median divided by RPC median, so higher is faster over RPC.")
    return "\n".join(lines) + "\n"


def print_table(results: list[dict]) -> None:
    print()
    print(f"{'operation':<32} {'CLI median':>12} {'RPC median':>12} {'CLI p95':>10} {'RPC p95':>10} {'speedup':>9}")
    print("-" * 94)
    for row in results:
        cli = row["cli"]["median"] if row["cli"]["n"] else None
        rpc = row["rpc"]["median"] if row["rpc"]["n"] else None
        speedup = (cli / rpc) if cli and rpc else None
        print(
            f"{row['label']:<32} "
            f"{(fmt_time(cli) if cli else 'n/a'):>12} "
            f"{(fmt_time(rpc) if rpc else 'n/a'):>12} "
            f"{(fmt_time(row['cli']['p95']) if cli else 'n/a'):>10} "
            f"{(fmt_time(row['rpc']['p95']) if rpc else 'n/a'):>10} "
            f"{(fmt_speedup(speedup) if speedup else 'n/a'):>9}"
        )
    for row in results:
        if row["cli_error"]:
            print(f"  {row['label']}: CLI failed: {row['cli_error']}")
        if row["rpc_error"]:
            print(f"  {row['label']}: RPC failed: {row['rpc_error']}")
    print()


def collect_env(cfg: Config) -> dict[str, str]:
    return {
        "repository": cfg.repo,
        "storage": cfg.storage,
        "runs": str(cfg.runs),
        "warmup": str(cfg.warmup),
        "os": platform.platform(),
        "python": platform.python_version(),
        "cpu_cores": str(os.cpu_count() or "unknown"),
        "cache_dir": str(cfg.cache_dir),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Benchmark ghx CLI calls against the JSON-RPC server.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument("--repo", default="TomzxCode/ghx", help="Target repository as OWNER/REPO")
    parser.add_argument("--ghx", help="Path to the ghx binary (default: PATH, then ./ghx)")
    parser.add_argument("--build", action="store_true", help="Build ghx from this repository first")
    parser.add_argument("--cache-dir", help="Cache directory to use (default: a populated temp dir)")
    parser.add_argument("--storage", choices=("file", "sqlite"), default="sqlite")
    parser.add_argument("--no-prepare", action="store_true", help="Do not populate the cache before timing")
    parser.add_argument(
        "--prepare",
        action="store_true",
        help="Populate an explicitly given --cache-dir before timing",
    )
    parser.add_argument("--runs", type=int, default=30, help="Timed runs per path per operation")
    parser.add_argument("--warmup", type=int, default=3, help="Discarded warmup runs per path")
    parser.add_argument("--timeout", type=float, default=180.0, help="Timeout per CLI call in seconds")
    parser.add_argument("--json", dest="json_path", help="Write raw results as JSON")
    parser.add_argument("-o", "--output", help="Write a Markdown report")
    args = parser.parse_args(argv)

    binary = build_binary() if args.build else resolve_binary(args.ghx)
    temp_dir: Path | None = None
    if args.cache_dir:
        cache_dir = Path(args.cache_dir).expanduser().resolve()
        prepare = args.prepare and not args.no_prepare
    else:
        temp_dir = Path(tempfile.mkdtemp(prefix="ghx-rpc-bench-"))
        cache_dir = temp_dir / "cache"
        prepare = not args.no_prepare
    cache_dir.mkdir(parents=True, exist_ok=True)

    port = free_port()
    cfg = Config(
        binary=binary,
        repo=args.repo,
        cache_dir=cache_dir,
        storage=args.storage,
        runs=args.runs,
        warmup=args.warmup,
        timeout=args.timeout,
        url=f"http://127.0.0.1:{port}",
        log_path=(temp_dir or Path(tempfile.gettempdir())) / "ghx-rpc-bench-server.log",
    )

    if prepare:
        prepare_cache(cfg)
    elif args.cache_dir:
        print("Using the given cache as-is; pass --prepare to populate it first.")

    issue, pr = discover_numbers(cfg)
    cases = make_cases(cfg, issue, pr)

    print(f"ghx:    {binary}")
    print(f"repo:   {cfg.repo}")
    print(f"cache:  {cfg.cache_dir}")
    print(f"server: {cfg.url}")

    log_file = open(cfg.log_path, "w", encoding="utf-8")
    server = start_server(cfg, log_file)
    results: list[dict] = []
    rpc = RPCClient(cfg.url)
    try:
        for case in cases:
            cli, http = benchmark(case, cfg, rpc)
            row = {
                "key": case.key,
                "label": case.label,
                "note": case.note,
                "cli": summarize(cli.durations),
                "rpc": summarize(http.durations),
                "cli_bytes": round(statistics.fmean(cli.bytes_read), 1) if cli.bytes_read else 0,
                "rpc_bytes": round(statistics.fmean(http.bytes_read), 1) if http.bytes_read else 0,
                "cli_error": cli.error,
                "rpc_error": http.error,
            }
            results.append(row)
    finally:
        rpc.close()
        server.terminate()
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()
        log_file.close()

    print_table(results)

    payload = {
        "environment": collect_env(cfg),
        "server_log": str(cfg.log_path),
        "results": results,
    }
    if args.json_path:
        Path(args.json_path).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
        print(f"JSON written to {args.json_path}")
    if args.output:
        Path(args.output).write_text(render_markdown(results, cfg), encoding="utf-8")
        print(f"Markdown written to {args.output}")
    if temp_dir is not None:
        print(f"Temp cache retained at {cfg.cache_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
