#!/usr/bin/env python3
"""Benchmark ghx against gh and render a self-contained HTML report.

ghx serves list/view commands from a local disk cache, while gh always talks to
the GitHub API. This script times equivalent commands from both tools across a
few cache scenarios (warm cache, cold cache, and a forced API refresh) and then
writes a single-file HTML report that answers the question "is ghx faster or
slower than gh?".

ghx caches to SQLite by default; pass --storage file (or sqlite) to benchmark a
specific backend. Run the script once per backend to compare them:

    python3 scripts/benchmark.py --repo cli/cli --storage sqlite -o sqlite.html
    python3 scripts/benchmark.py --repo cli/cli --storage file   -o file.html

Only read-only commands are executed. ghx is always pointed at a throwaway
cache directory so the user's real cache at ~/.cache/ghx is never touched.

Typical usage::

    python3 scripts/benchmark.py --repo TomzxCode/gh-cached
    python3 scripts/benchmark.py --repo cli/cli --runs 7 -o cli.html
    python3 scripts/benchmark.py --repo cli/cli --storage file   # benchmark file backend
    python3 scripts/benchmark.py --demo          # render a sample report, no network

The report is written to benchmark-report.html by default.
"""

from __future__ import annotations

import argparse
import html
import json
import math
import os
import platform
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUTPUT = "benchmark-report.html"
LIMIT = 100


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


@dataclass
class Sample:
    """One timed execution of a command."""

    seconds: float
    returncode: int
    stdout_bytes: int
    stderr: str = ""


@dataclass
class BenchCase:
    """A pair of equivalent ghx/gh commands plus its cache scenario."""

    key: str
    label: str
    description: str
    category: str  # "issue" | "pr"
    cache: str  # "warm" | "cold" | "refresh"
    ghx_args: list[str]
    gh_args: list[str]
    parity: str = "different"  # "equal" | "ghx-less" | "ghx-more" | "different"
    parity_note: str = ""


# How the two commands' workloads relate. Surfaced in the report so a speedup is
# not read as a like-for-like win when the tools are doing different amounts of
# work.
PARITY_META: dict[str, tuple[str, str]] = {
    "equal": ("Equal work", "parity-equal"),
    "ghx-less": ("ghx does less", "parity-less"),
    "ghx-more": ("ghx does more", "parity-more"),
    "different": ("Different work", "parity-different"),
}


@dataclass
class ToolResult:
    """Collected samples and error state for one tool on one case."""

    tool: str
    samples: list[Sample] = field(default_factory=list)
    error: str | None = None

    @property
    def ok(self) -> bool:
        return self.error is None and bool(self.samples)

    @property
    def durations(self) -> list[float]:
        return [s.seconds for s in self.samples]


@dataclass
class CaseResult:
    """The outcome of running one case against both tools."""

    case: BenchCase
    ghx: ToolResult
    gh: ToolResult

    @property
    def status(self) -> str:
        if self.ghx.ok and self.gh.ok:
            return "ok"
        if not self.ghx.ok and not self.gh.ok:
            return "error"
        return "partial"


@dataclass
class Config:
    repo: str
    ghx_bin: Path
    gh_bin: Path
    storage: str  # "file" | "sqlite" - the ghx cache backend under test
    runs: int
    warmup: int
    timeout: float
    cache_timeout: float
    work_dir: Path
    warm_dir: Path
    cold_root: Path
    only: re.Pattern[str] | None

    def cache_dir_for(self, case: BenchCase, phase: str, run_index: int) -> Path:
        if case.cache == "cold":
            return self.cold_root / f"{case.key}-{phase}-{run_index}"
        return self.warm_dir


# ---------------------------------------------------------------------------
# Command execution and statistics
# ---------------------------------------------------------------------------

BASE_ENV = {
    "GH_PAGER": "cat",
    "PAGER": "cat",
    "GH_NO_UPDATE_NOTIFIER": "1",
    "GH_PROMPT_DISABLED": "1",
    "NO_COLOR": "1",
    "CLICOLOR": "0",
    "CLICOLOR_FORCE": "0",
    "TERM": "dumb",
}


def build_cmd(
    case: BenchCase, tool: str, cfg: Config, phase: str, run_index: int
) -> list[str]:
    """Build the argv for one tool/case/scenario combination."""
    if tool == "ghx":
        cache_dir = cfg.cache_dir_for(case, phase, run_index)
        return [
            str(cfg.ghx_bin),
            "--cache-dir",
            str(cache_dir),
            "--storage",
            cfg.storage,
            *case.ghx_args,
        ]
    return [str(cfg.gh_bin), *case.gh_args]


def execute(
    case: BenchCase, tool: str, cfg: Config, phase: str, run_index: int
) -> Sample:
    """Run one command once and return a timed sample. Raises on failure."""
    cmd = build_cmd(case, tool, cfg, phase, run_index)
    if case.cache == "cold" and tool == "ghx":
        cache_dir = cfg.cache_dir_for(case, phase, run_index)
        shutil.rmtree(cache_dir, ignore_errors=True)
        cache_dir.mkdir(parents=True, exist_ok=True)

    env = {**os.environ, **BASE_ENV}
    start = time.perf_counter()
    proc = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        errors="replace",
        timeout=cfg.timeout,
        env=env,
        check=False,
    )
    elapsed = time.perf_counter() - start
    stderr = (proc.stderr or "").strip()
    if proc.returncode != 0:
        raise RuntimeError(f"exit {proc.returncode}: {stderr[:300] or 'no stderr'}")
    return Sample(
        seconds=elapsed,
        returncode=proc.returncode,
        stdout_bytes=len(proc.stdout or ""),
        stderr=stderr,
    )


def percentile(sorted_vals: list[float], p: float) -> float:
    """Linear-interpolated percentile of an already sorted list."""
    if not sorted_vals:
        return 0.0
    if len(sorted_vals) == 1:
        return sorted_vals[0]
    k = (len(sorted_vals) - 1) * p / 100.0
    lo = math.floor(k)
    hi = math.ceil(k)
    if lo == hi:
        return sorted_vals[lo]
    return sorted_vals[lo] + (sorted_vals[hi] - sorted_vals[lo]) * (k - lo)


def summarize(durations: list[float]) -> dict[str, float]:
    """Compute the summary statistics used throughout the report."""
    if not durations:
        return {
            "n": 0,
            "min": 0.0,
            "max": 0.0,
            "mean": 0.0,
            "median": 0.0,
            "stdev": 0.0,
            "p95": 0.0,
        }
    ordered = sorted(durations)
    return {
        "n": float(len(ordered)),
        "min": ordered[0],
        "max": ordered[-1],
        "mean": statistics.fmean(ordered),
        "median": statistics.median(ordered),
        "stdev": statistics.pstdev(ordered) if len(ordered) > 1 else 0.0,
        "p95": percentile(ordered, 95),
    }


def fmt_time(seconds: float) -> str:
    """Human-friendly duration formatting."""
    ms = seconds * 1000.0
    if ms < 1.0:
        return f"{seconds * 1_000_000:.0f} \u00b5s"
    if ms < 1000.0:
        return f"{ms:.1f} ms"
    return f"{ms / 1000.0:.2f} s"


def fmt_speedup(speedup: float) -> str:
    """Format a gh-mediated speedup ratio (gh time / ghx time)."""
    if speedup >= 1.0:
        return f"{speedup:.2f}\u00d7 faster"
    return f"{1.0 / speedup:.2f}\u00d7 slower"


# ---------------------------------------------------------------------------
# Case construction and running
# ---------------------------------------------------------------------------


def run_gh_json(gh_bin: Path, args: list[str], timeout: float) -> object:
    """Run a gh command returning JSON and parse it."""
    env = {**os.environ, **BASE_ENV}
    proc = subprocess.run(
        [str(gh_bin), *args],
        capture_output=True,
        text=True,
        errors="replace",
        timeout=timeout,
        env=env,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError((proc.stderr or "").strip()[:300] or "gh command failed")
    return json.loads(proc.stdout or "[]")


def detect_repo(cwd: Path) -> str | None:
    """Best-effort detection of OWNER/REPO from the origin remote."""
    try:
        proc = subprocess.run(
            ["git", "remote", "get-url", "origin"],
            capture_output=True,
            text=True,
            cwd=cwd,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):  # fmt: skip
        return None
    if proc.returncode != 0:
        return None
    url = proc.stdout.strip()
    match = re.search(r"github\.com[:/]+([^/]+/[^/]+?)(?:\.git)?$", url)
    return match.group(1) if match else None


def make_cases(
    repo: str, issue_number: int | None, pr_number: int | None
) -> list[BenchCase]:
    """Build the benchmark matrix for a repository."""
    r = ["--repo", repo]
    cases: list[BenchCase] = [
        BenchCase(
            key="issue-list-warm",
            label="Issue list (warm cache)",
            description="List all issues from a pre-populated local cache versus a live gh fetch.",
            category="issue",
            cache="warm",
            ghx_args=["issue", "list", "--state", "all", "--limit", str(LIMIT), *r],
            gh_args=["issue", "list", "--state", "all", "--limit", str(LIMIT), *r],
            parity="different",
            parity_note="ghx reads a pre-populated on-disk cache; gh fetches the same page over the network. This measures the cache payoff, not identical work.",
        ),
        BenchCase(
            key="pr-list-warm",
            label="PR list (warm cache)",
            description="List all pull requests from a pre-populated local cache versus a live gh fetch.",
            category="pr",
            cache="warm",
            ghx_args=["pr", "list", "--state", "all", "--limit", str(LIMIT), *r],
            gh_args=["pr", "list", "--state", "all", "--limit", str(LIMIT), *r],
            parity="different",
            parity_note="ghx reads a pre-populated on-disk cache; gh fetches the same page over the network. This measures the cache payoff, not identical work.",
        ),
        BenchCase(
            key="issue-list-cold",
            label="Issue list (cold cache)",
            description="Empty local cache: ghx falls back to the API, matching gh's starting point.",
            category="issue",
            cache="cold",
            ghx_args=["issue", "list", "--state", "all", "--limit", str(LIMIT), *r],
            gh_args=["issue", "list", "--state", "all", "--limit", str(LIMIT), *r],
            parity="ghx-more",
            parity_note="ghx fetches and stores every issue (all states, with comments) before filtering in memory; gh fetches a single filtered page. ghx is expected to be slower here.",
        ),
        BenchCase(
            key="pr-list-cold",
            label="PR list (cold cache)",
            description="Empty local cache: ghx fetches everything from the API before answering.",
            category="pr",
            cache="cold",
            ghx_args=["pr", "list", "--state", "all", "--limit", str(LIMIT), *r],
            gh_args=["pr", "list", "--state", "all", "--limit", str(LIMIT), *r],
            parity="ghx-more",
            parity_note="ghx fetches and caches every pull request (all states, with reviews and comments) before filtering; gh fetches a single filtered page. ghx is expected to be slower here.",
        ),
    ]

    if issue_number is not None:
        cases.extend(
            [
                BenchCase(
                    key="issue-view-warm",
                    label=f"Issue view #{issue_number} (warm cache)",
                    description="View one issue with comments, served from the local cache.",
                    category="issue",
                    cache="warm",
                    ghx_args=["issue", "view", str(issue_number), "--comments", *r],
                    gh_args=["issue", "view", str(issue_number), "--comments", *r],
                    parity="different",
                    parity_note="ghx opens one cached JSON file for the issue; gh issues its full view API call. This measures the cache payoff, not identical work.",
                ),
                BenchCase(
                    key="issue-view-refresh",
                    label=f"Issue view #{issue_number} (forced refresh)",
                    description="--refresh bypasses the cache so both tools hit the API: overhead-only comparison.",
                    category="issue",
                    cache="refresh",
                    ghx_args=[
                        "issue",
                        "view",
                        str(issue_number),
                        "--comments",
                        "--refresh",
                        *r,
                    ],
                    gh_args=["issue", "view", str(issue_number), "--comments", *r],
                    parity="ghx-less",
                    parity_note="Both hit the API, but ghx sends a leaner GraphQL query and fetches no project items, reactions, sub-issues or blocking relations, all of which gh requests.",
                ),
            ]
        )

    if pr_number is not None:
        cases.extend(
            [
                BenchCase(
                    key="pr-view-warm",
                    label=f"PR view #{pr_number} (warm cache)",
                    description="View one pull request with comments, served from the local cache.",
                    category="pr",
                    cache="warm",
                    ghx_args=["pr", "view", str(pr_number), "--comments", *r],
                    gh_args=["pr", "view", str(pr_number), "--comments", *r],
                    parity="different",
                    parity_note="ghx opens one cached JSON file for the PR; gh issues its full view API call. This measures the cache payoff, not identical work.",
                ),
                BenchCase(
                    key="pr-view-refresh",
                    label=f"PR view #{pr_number} (forced refresh)",
                    description="--refresh bypasses the cache so both tools hit the API: overhead-only comparison.",
                    category="pr",
                    cache="refresh",
                    ghx_args=[
                        "pr",
                        "view",
                        str(pr_number),
                        "--comments",
                        "--refresh",
                        *r,
                    ],
                    gh_args=["pr", "view", str(pr_number), "--comments", *r],
                    parity="ghx-less",
                    parity_note="Both hit the API, but ghx sends a leaner GraphQL query and skips project items and the status-check rollup, both of which gh fetches.",
                ),
            ]
        )

    return cases


def populate_warm_cache(cfg: Config) -> tuple[bool, str]:
    """Fetch everything into the warm cache directory once."""
    cmd = [
        str(cfg.ghx_bin),
        "--cache-dir",
        str(cfg.warm_dir),
        "--storage",
        cfg.storage,
        "cache",
        "--repo",
        cfg.repo,
    ]
    env = {**os.environ, **BASE_ENV}
    try:
        proc = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            errors="replace",
            timeout=cfg.cache_timeout,
            env=env,
            check=False,
        )
    except subprocess.TimeoutExpired:
        return False, f"timed out after {cfg.cache_timeout:.0f}s"
    if proc.returncode != 0:
        return False, (proc.stderr or proc.stdout or "").strip()[:300]
    return True, ""


def run_case(case: BenchCase, cfg: Config) -> CaseResult:
    """Run warmups then interleaved timed samples for one case."""
    ghx_res = ToolResult(tool="ghx")
    gh_res = ToolResult(tool="gh")
    failure: dict[str, str] = {}

    def attempt(tool: str, phase: str, run_index: int) -> Sample | None:
        try:
            return execute(case, tool, cfg, phase, run_index)
        except subprocess.TimeoutExpired:
            failure[tool] = f"timed out after {cfg.timeout:.0f}s"
        except Exception as exc:  # noqa: BLE001 - surfacing any failure is the point
            failure[tool] = str(exc)
        return None

    for w in range(cfg.warmup):
        for tool in ("ghx", "gh"):
            attempt(tool, "warmup", w)

    for i in range(cfg.runs):
        order = ("ghx", "gh") if i % 2 == 0 else ("gh", "ghx")
        for tool in order:
            sample = attempt(tool, "timed", i)
            if sample is not None:
                (ghx_res if tool == "ghx" else gh_res).samples.append(sample)

    for tool, res in (("ghx", ghx_res), ("gh", gh_res)):
        if len(res.samples) < cfg.runs:
            res.error = failure.get(
                tool, f"only {len(res.samples)}/{cfg.runs} runs succeeded"
            )
    return CaseResult(case=case, ghx=ghx_res, gh=gh_res)


# ---------------------------------------------------------------------------
# HTML rendering
# ---------------------------------------------------------------------------

CSS = r"""
:root {
  --bg: #f6f7fb;
  --panel: #ffffff;
  --panel-2: #f0f2f8;
  --text: #1b1f2a;
  --muted: #6b7280;
  --border: #e3e6ef;
  --ghx: #6366f1;
  --ghx-soft: rgba(99, 102, 241, 0.14);
  --gh: #94a3b8;
  --gh-soft: rgba(148, 163, 184, 0.2);
  --good: #10b981;
  --bad: #ef4444;
  --shadow: 0 1px 2px rgba(16, 24, 40, 0.04), 0 8px 24px rgba(16, 24, 40, 0.06);
}
[data-theme="dark"] {
  --bg: #0b0e17;
  --panel: #131826;
  --panel-2: #1a2133;
  --text: #e8ecf6;
  --muted: #98a2b8;
  --border: #262f45;
  --ghx: #818cf8;
  --ghx-soft: rgba(129, 140, 248, 0.18);
  --gh: #64748b;
  --gh-soft: rgba(100, 116, 139, 0.24);
  --good: #34d399;
  --bad: #f87171;
  --shadow: 0 1px 2px rgba(0, 0, 0, 0.35), 0 12px 32px rgba(0, 0, 0, 0.35);
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}
code, .mono { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace; }
.wrap { max-width: 1120px; margin: 0 auto; padding: 32px 20px 80px; }
header.hero {
  position: relative;
  overflow: hidden;
  border-radius: 18px;
  padding: 30px 32px;
  color: #fff;
  background: linear-gradient(125deg, #4f46e5 0%, #7c3aed 45%, #0ea5e9 100%);
  box-shadow: var(--shadow);
}
header.hero::after {
  content: "";
  position: absolute;
  right: -80px; top: -120px;
  width: 340px; height: 340px;
  background: radial-gradient(circle, rgba(255,255,255,0.28), transparent 65%);
}
header.hero h1 { margin: 0 0 6px; font-size: 1.7rem; letter-spacing: -0.02em; }
header.hero p { margin: 0; opacity: 0.92; max-width: 70ch; }
.hero-meta { margin-top: 16px; display: flex; flex-wrap: wrap; gap: 8px; }
.pill {
  display: inline-flex; align-items: center; gap: 6px;
  background: rgba(255,255,255,0.16); border: 1px solid rgba(255,255,255,0.22);
  padding: 4px 11px; border-radius: 999px; font-size: 0.8rem;
}
.pill strong { font-weight: 600; }
.toolbar { display: flex; justify-content: flex-end; margin: 14px 0 -10px; }
button.theme-toggle {
  border: 1px solid var(--border); background: var(--panel); color: var(--text);
  border-radius: 10px; padding: 7px 13px; cursor: pointer; font-size: 0.85rem; box-shadow: var(--shadow);
}
.section { margin-top: 34px; }
.section > h2 {
  font-size: 1.15rem; margin: 0 0 4px; letter-spacing: -0.01em;
}
.section > .sub { color: var(--muted); font-size: 0.88rem; margin-bottom: 16px; }
.verdict {
  display: grid; grid-template-columns: 1.15fr 2fr; gap: 18px; margin-top: 24px;
}
@media (max-width: 760px) { .verdict { grid-template-columns: 1fr; } }
.card {
  background: var(--panel); border: 1px solid var(--border); border-radius: 16px;
  padding: 20px; box-shadow: var(--shadow);
}
.verdict .headline { display: flex; flex-direction: column; justify-content: center; }
.verdict .big { font-size: 2.4rem; font-weight: 800; letter-spacing: -0.03em; line-height: 1.1; }
.verdict .big.faster { color: var(--good); }
.verdict .big.slower { color: var(--bad); }
.verdict .big.mixed { color: var(--muted); }
.verdict .caption { color: var(--muted); font-size: 0.9rem; margin-top: 6px; }
.stat-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
@media (max-width: 560px) { .stat-grid { grid-template-columns: 1fr 1fr; } }
.stat { background: var(--panel-2); border-radius: 12px; padding: 12px 14px; }
.stat .k { color: var(--muted); font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.06em; }
.stat .v { font-size: 1.15rem; font-weight: 700; margin-top: 3px; }
.cases { display: grid; grid-template-columns: 1fr; gap: 16px; }
.case-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.case-head h3 { margin: 0; font-size: 1rem; }
.case-desc { color: var(--muted); font-size: 0.84rem; margin: 4px 0 14px; }
.badge {
  font-size: 0.78rem; font-weight: 600; padding: 4px 10px; border-radius: 999px; white-space: nowrap;
}
.badge.faster { color: #065f46; background: rgba(16,185,129,0.16); }
.badge.slower { color: #991b1b; background: rgba(239,68,68,0.15); }
[data-theme="dark"] .badge.faster { color: #6ee7b7; }
[data-theme="dark"] .badge.slower { color: #fca5a5; }
.badge.neutral { color: var(--muted); background: var(--panel-2); }
.badge-group { display: inline-flex; flex-wrap: wrap; gap: 6px; justify-content: flex-end; }
.badge.parity-equal { color: #065f46; background: rgba(16, 185, 129, 0.16); }
.badge.parity-less { color: #1e40af; background: rgba(59, 130, 246, 0.15); }
.badge.parity-more { color: #92400e; background: rgba(245, 158, 11, 0.18); }
.badge.parity-different { color: #334155; background: rgba(100, 116, 139, 0.16); }
[data-theme="dark"] .badge.parity-equal { color: #6ee7b7; }
[data-theme="dark"] .badge.parity-less { color: #93c5fd; }
[data-theme="dark"] .badge.parity-more { color: #fcd34d; }
[data-theme="dark"] .badge.parity-different { color: #cbd5e1; }
.fairness {
  margin: 0 0 14px; padding: 9px 12px; border-radius: 8px;
  background: var(--panel-2); border-left: 3px solid var(--border);
  font-size: 0.82rem; color: var(--muted);
}
.fairness.parity-equal { border-left-color: var(--good); }
.fairness.parity-less { border-left-color: #3b82f6; }
.fairness.parity-more { border-left-color: #f59e0b; }
.fairness.parity-different { border-left-color: #94a3b8; }
.charts-row { display: grid; grid-template-columns: 1.15fr 0.85fr; gap: 20px; align-items: center; }
@media (max-width: 760px) { .charts-row { grid-template-columns: 1fr; gap: 14px; } }
.legend { display: flex; flex-wrap: wrap; gap: 8px 16px; margin: 2px 0 14px; font-size: 0.8rem; color: var(--muted); }
.legend span { display: inline-flex; align-items: center; gap: 7px; }
.swatch { width: 11px; height: 11px; border-radius: 3px; display: inline-block; flex: none; }
.swatch.ghx { background: var(--ghx); }
.swatch.gh { background: var(--gh); }

/* Median comparison bars. Plain HTML (not SVG) so labels keep their real size. */
.bars { display: flex; flex-direction: column; gap: 10px; }
.bar-row, .dist-row { display: grid; grid-template-columns: 36px minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.bar-key { font-size: 0.78rem; color: var(--muted); }
.bar-track { position: relative; height: 22px; background: var(--panel-2); border-radius: 6px; overflow: hidden; }
.bar-fill { display: block; height: 100%; border-radius: 6px; min-width: 2px; }
.bar-fill.ghx { background: var(--ghx); }
.bar-fill.gh { background: var(--gh); }
.bar-val { text-align: right; font-size: 0.8rem; font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; }

/* Sample distribution strip plot. */
.dist { display: flex; flex-direction: column; gap: 12px; }
.dist-track { position: relative; height: 26px; background: var(--panel-2); border-radius: 6px; }
.dist-dot { position: absolute; top: 50%; width: 8px; height: 8px; border-radius: 50%; transform: translate(-50%, -50%); }
.dist-dot.ghx { background: var(--ghx); }
.dist-dot.gh { background: var(--gh); }
.dist-median { position: absolute; top: 3px; bottom: 3px; width: 2px; border-radius: 1px; transform: translateX(-50%); }
.dist-median.ghx { background: var(--ghx); }
.dist-median.gh { background: var(--gh); }

/* Speedup-by-scenario bars (log scale, parity centred). */
.lolli { display: flex; flex-direction: column; gap: 14px; }
.lolli-row { display: grid; grid-template-columns: minmax(0, 200px) minmax(0, 1fr) 124px; align-items: center; column-gap: 12px; }
.lolli-label { font-size: 0.84rem; overflow-wrap: anywhere; }
.lolli-track { position: relative; height: 10px; background: var(--panel-2); border-radius: 999px; }
.lolli-parity { position: absolute; top: -4px; bottom: -4px; width: 1px; background: var(--border); }
.lolli-span { position: absolute; top: 0; height: 10px; border-radius: 999px; }
.lolli-span.faster { background: var(--good); }
.lolli-span.slower { background: var(--bad); }
.lolli-dot { position: absolute; top: 50%; width: 13px; height: 13px; border-radius: 50%; transform: translate(-50%, -50%); border: 2px solid var(--panel); }
.lolli-dot.faster { background: var(--good); }
.lolli-dot.slower { background: var(--bad); }
.lolli-value { text-align: right; font-size: 0.8rem; font-weight: 600; white-space: nowrap; }
.lolli-value.faster { color: var(--good); }
.lolli-value.slower { color: var(--bad); }

@media (max-width: 680px) {
  .lolli-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 7px; }
  .lolli-label { grid-column: 1; grid-row: 1; }
  .lolli-value { grid-column: 2; grid-row: 1; }
  .lolli-track { grid-column: 1 / -1; grid-row: 2; }
}
@media (max-width: 640px) {
  .wrap { padding: 20px 14px 60px; }
  header.hero { padding: 22px 18px; border-radius: 14px; }
  header.hero h1 { font-size: 1.32rem; }
  header.hero p { font-size: 0.92rem; }
  .section { margin-top: 26px; }
  .card { padding: 16px; }
  .verdict { margin-top: 18px; }
  .verdict .big { font-size: 1.9rem; }
  .bar-val, .lolli-value { font-size: 0.76rem; }
  .bar-row, .dist-row { grid-template-columns: 32px minmax(0, 1fr) auto; gap: 8px; }
}
.note {
  background: var(--panel-2); border-left: 3px solid var(--border); border-radius: 8px;
  padding: 10px 14px; font-size: 0.84rem; color: var(--muted);
}
table { width: 100%; border-collapse: collapse; font-size: 0.86rem; }
th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid var(--border); }
th { color: var(--muted); font-weight: 600; font-size: 0.74rem; text-transform: uppercase; letter-spacing: 0.05em; cursor: pointer; user-select: none; }
th[data-sort]:hover { color: var(--text); }
tbody tr:hover { background: var(--panel-2); }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
.table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: 16px; background: var(--panel); box-shadow: var(--shadow); }
.table-wrap table td, .table-wrap table th { padding-left: 16px; padding-right: 16px; }
.env-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 10px 24px; }
.env-grid .row { display: flex; justify-content: space-between; gap: 12px; border-bottom: 1px dashed var(--border); padding: 7px 0; font-size: 0.86rem; }
.env-grid .row .k { color: var(--muted); }
details { margin-top: 12px; }
summary { cursor: pointer; color: var(--muted); font-size: 0.86rem; }
pre {
  background: var(--panel-2); border: 1px solid var(--border); border-radius: 10px;
  padding: 14px; overflow-x: auto; font-size: 0.8rem; margin-top: 10px;
}
footer { margin-top: 44px; color: var(--muted); font-size: 0.8rem; text-align: center; }
.fade { animation: fade-up 0.5s ease; }
@keyframes fade-up { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }
@media (prefers-reduced-motion: reduce) {
  .fade { animation: none; }
  * { scroll-behavior: auto !important; }
}
@media print {
  body { background: #fff; }
  .toolbar, button.theme-toggle { display: none; }
  .card, .table-wrap, header.hero { box-shadow: none; }
}
"""

JS = r"""
(function () {
  var root = document.documentElement;
  var saved = localStorage.getItem("ghx-bench-theme");
  if (saved) { root.setAttribute("data-theme", saved); }
  var btn = document.getElementById("theme-toggle");
  if (btn) {
    var sync = function () {
      var dark = root.getAttribute("data-theme") === "dark";
      btn.textContent = dark ? "Light mode" : "Dark mode";
    };
    sync();
    btn.addEventListener("click", function () {
      var dark = root.getAttribute("data-theme") === "dark";
      if (dark) { root.removeAttribute("data-theme"); } else { root.setAttribute("data-theme", "dark"); }
      localStorage.setItem("ghx-bench-theme", root.getAttribute("data-theme") || "light");
      sync();
    });
  }

  document.querySelectorAll("table[data-sortable]").forEach(function (table) {
    var headers = table.querySelectorAll("th[data-sort]");
    headers.forEach(function (th, index) {
      th.addEventListener("click", function () {
        var tbody = table.querySelector("tbody");
        var rows = Array.prototype.slice.call(tbody.querySelectorAll("tr"));
        var numeric = th.getAttribute("data-sort") === "num";
        var dir = th.getAttribute("data-dir") === "asc" ? -1 : 1;
        headers.forEach(function (h) { h.removeAttribute("data-dir"); });
        th.setAttribute("data-dir", dir === 1 ? "asc" : "desc");
        rows.sort(function (a, b) {
          var av = a.children[index].getAttribute("data-value") || a.children[index].textContent.trim();
          var bv = b.children[index].getAttribute("data-value") || b.children[index].textContent.trim();
          if (numeric) { return (parseFloat(av) - parseFloat(bv)) * dir; }
          return av.localeCompare(bv) * dir;
        });
        rows.forEach(function (row) { tbody.appendChild(row); });
      });
    });
  });
})();
"""


def esc(text: object) -> str:
    return html.escape(str(text), quote=True)


def render_case_bars(ghx_seconds: float, gh_seconds: float) -> str:
    """Horizontal bars comparing the two medians for one case (HTML/CSS)."""
    max_seconds = max(ghx_seconds, gh_seconds, 1e-9)

    def row(label: str, seconds: float, cls: str) -> str:
        pct = max(0.6, seconds / max_seconds * 100.0)
        return (
            '<div class="bar-row">'
            f'<span class="bar-key">{esc(label)}</span>'
            f'<span class="bar-track"><span class="bar-fill {cls}" style="width:{pct:.2f}%"></span></span>'
            f'<span class="bar-val">{esc(fmt_time(seconds))}</span>'
            "</div>"
        )

    return (
        '<div class="bars" role="img" aria-label="Median runtime comparison">'
        + row("ghx", ghx_seconds, "ghx")
        + row("gh", gh_seconds, "gh")
        + "</div>"
    )


def render_distribution(ghx_samples: list[float], gh_samples: list[float]) -> str:
    """Strip plot of the raw samples for one case on a shared scale (HTML/CSS)."""
    all_samples = [*ghx_samples, *gh_samples]
    if not all_samples:
        return ""
    lo, hi = min(all_samples), max(all_samples)
    span = hi - lo

    def pos(value: float) -> float:
        if span <= 0:
            return 50.0
        return (value - lo) / span * 100.0

    def row(label: str, samples: list[float], cls: str) -> str:
        if not samples:
            return ""
        ordered = sorted(samples)
        median = statistics.median(ordered)
        dots = []
        for i, sample in enumerate(samples):
            # Nudge coincident points apart so overlapping samples stay visible.
            offset = -6 if i % 2 else 6
            dots.append(
                f'<span class="dist-dot {cls}" style="left:{pos(sample):.2f}%;margin-top:{offset}px"></span>'
            )
        return (
            '<div class="dist-row">'
            f'<span class="bar-key">{esc(label)}</span>'
            f'<span class="dist-track">'
            f'<span class="dist-median {cls}" style="left:{pos(median):.2f}%"></span>'
            + "".join(dots)
            + "</span>"
            f'<span class="bar-val">{esc(fmt_time(ordered[0]))}\u2013{esc(fmt_time(ordered[-1]))}</span>'
            "</div>"
        )

    return (
        '<div class="dist" role="img" aria-label="Sample distribution">'
        + row("ghx", ghx_samples, "ghx")
        + row("gh", gh_samples, "gh")
        + "</div>"
    )


def render_speedup_chart(cases: list[dict]) -> str:
    """Per-case speedup ratios as HTML bars on a log scale centred on parity."""
    scored = [c for c in cases if c["status"] == "ok" and c["speedup"] > 0]
    if not scored:
        return ""
    logs = [math.log10(c["speedup"]) for c in scored]
    lo = min(logs + [0.0])
    hi = max(logs + [0.0])
    pad = max((hi - lo) * 0.08, 0.05)
    lo -= pad
    hi += pad
    span = hi - lo

    def pos(ratio: float) -> float:
        return (math.log10(ratio) - lo) / span * 100.0

    parity = pos(1.0)
    rows = []
    for case in scored:
        ratio = case["speedup"]
        point = pos(ratio)
        cls = "faster" if ratio >= 1.0 else "slower"
        left = min(parity, point)
        width = max(abs(point - parity), 0.6)
        rows.append(
            '<div class="lolli-row">'
            f'<span class="lolli-label">{esc(case["label"])}</span>'
            f'<span class="lolli-track">'
            f'<span class="lolli-parity" style="left:{parity:.2f}%"></span>'
            f'<span class="lolli-span {cls}" style="left:{left:.2f}%;width:{width:.2f}%"></span>'
            f'<span class="lolli-dot {cls}" style="left:{point:.2f}%"></span>'
            "</span>"
            f'<span class="lolli-value {cls}">{esc(fmt_speedup(ratio))}</span>'
            "</div>"
        )
    return (
        '<div class="lolli" role="img" aria-label="Speedup per case">'
        + "".join(rows)
        + "</div>"
    )


def build_context(
    results: list[CaseResult], cfg: Config, env_info: dict[str, str]
) -> dict:
    """Turn raw results into a render-ready dictionary."""
    cases: list[dict] = []
    for result in results:
        ghx_stats = summarize(result.ghx.durations)
        gh_stats = summarize(result.gh.durations)
        status = result.status
        speedup = 0.0
        if status == "ok" and ghx_stats["median"] > 0:
            speedup = gh_stats["median"] / ghx_stats["median"]
        cases.append(
            {
                "key": result.case.key,
                "label": result.case.label,
                "description": result.case.description,
                "category": result.case.category,
                "cache": result.case.cache,
                "status": status,
                "ghx": ghx_stats,
                "gh": gh_stats,
                "ghx_samples": result.ghx.durations,
                "gh_samples": result.gh.durations,
                "ghx_error": result.ghx.error,
                "gh_error": result.gh.error,
                "speedup": speedup,
                "ghx_faster": speedup >= 1.0,
                "delta": ghx_stats["median"] - gh_stats["median"],
                "parity": result.case.parity,
                "parity_label": PARITY_META.get(
                    result.case.parity, PARITY_META["different"]
                )[0],
                "parity_class": PARITY_META.get(
                    result.case.parity, PARITY_META["different"]
                )[1],
                "parity_note": result.case.parity_note,
                "chart": render_case_bars(ghx_stats["median"], gh_stats["median"])
                if status == "ok"
                else "",
                "box": render_distribution(result.ghx.durations, result.gh.durations)
                if status == "ok"
                else "",
            }
        )

    scored = [c for c in cases if c["status"] == "ok" and c["ghx"]["median"] > 0]
    total_ghx = sum(c["ghx"]["median"] for c in scored)
    total_gh = sum(c["gh"]["median"] for c in scored)
    weighted_speedup = (total_gh / total_ghx) if total_ghx > 0 else 0.0
    mean_speedup = statistics.fmean([c["speedup"] for c in scored]) if scored else 0.0
    wins = sum(1 for c in scored if c["speedup"] >= 1.0)
    losses = len(scored) - wins

    if not scored:
        verdict = {"available": False}
    else:
        pct = abs(weighted_speedup - 1.0) * 100.0
        verdict = {
            "available": True,
            "faster": weighted_speedup >= 1.0,
            "pct": pct,
            "weighted_speedup": weighted_speedup,
            "mean_speedup": mean_speedup,
            "wins": wins,
            "losses": losses,
            "scored": len(scored),
            "total_ghx": total_ghx,
            "total_gh": total_gh,
            "gain_label": "Saved per 100 runs"
            if weighted_speedup >= 1.0
            else "Lost per 100 runs",
            "gain": abs(total_gh - total_ghx) * 100.0,
        }

    non_equal = [c for c in cases if c["status"] == "ok" and c["parity"] != "equal"]
    charted = [c for c in cases if c["status"] == "ok"]
    if charted and len(non_equal) == len(charted):
        chart_caveat = "Every scenario above compares different workloads. Read the fairness note on each card before treating a ratio as a like-for-like win."
    elif non_equal:
        chart_caveat = f"{len(non_equal)} of {len(charted)} scenarios above compare different workloads; their fairness notes explain how."
    else:
        chart_caveat = ""

    return {
        "repo": cfg.repo,
        "storage": cfg.storage,
        "runs": cfg.runs,
        "warmup": cfg.warmup,
        "generated_at": datetime.now(UTC).strftime("%Y-%m-%d %H:%M UTC"),
        "env": env_info,
        "verdict": verdict,
        "cases": cases,
        "speedup_chart": render_speedup_chart(cases),
        "chart_caveat": chart_caveat,
        "scored": len(scored),
        "raw_json": json.dumps(
            [
                {
                    "case": c["key"],
                    "label": c["label"],
                    "status": c["status"],
                    "ghx_storage_backend": cfg.storage,
                    "speedup_gh_over_ghx": round(c["speedup"], 4),
                    "ghx_median_seconds": round(c["ghx"]["median"], 6),
                    "gh_median_seconds": round(c["gh"]["median"], 6),
                    "ghx_samples_seconds": [round(s, 6) for s in c["ghx_samples"]],
                    "gh_samples_seconds": [round(s, 6) for s in c["gh_samples"]],
                }
                for c in cases
            ],
            indent=2,
        ),
    }


def render_case_card(case: dict) -> str:
    if case["status"] != "ok":
        error_bits = []
        if case["ghx_error"]:
            error_bits.append(f"ghx: {esc(case['ghx_error'])}")
        if case["gh_error"]:
            error_bits.append(f"gh: {esc(case['gh_error'])}")
        reason = " &middot; ".join(error_bits) or "no successful runs"
        return f"""
        <div class="card fade">
          <div class="case-head">
            <h3>{esc(case["label"])}</h3>
            <span class="badge neutral">skipped</span>
          </div>
          <div class="case-desc">{esc(case["description"])}</div>
          <div class="note">Not benchmarked: {reason}</div>
        </div>"""

    badge_class = "faster" if case["ghx_faster"] else "slower"
    return f"""
    <div class="card fade">
      <div class="case-head">
        <h3>{esc(case["label"])}</h3>
        <span class="badge-group">
          <span class="badge {case["parity_class"]}">{esc(case["parity_label"])}</span>
          <span class="badge {badge_class}">ghx {esc(fmt_speedup(case["speedup"]))}</span>
        </span>
      </div>
      <div class="case-desc">{esc(case["description"])}</div>
      <div class="fairness {case["parity_class"]}">{esc(case["parity_note"])}</div>
      <div class="legend">
        <span><i class="swatch ghx"></i>ghx median {esc(fmt_time(case["ghx"]["median"]))}</span>
        <span><i class="swatch gh"></i>gh median {esc(fmt_time(case["gh"]["median"]))}</span>
      </div>
      <div class="charts-row">
        <div>{case["chart"]}</div>
        <div>{case["box"]}</div>
      </div>
    </div>"""


def render_report(ctx: dict) -> str:
    verdict = ctx["verdict"]
    if verdict.get("available"):
        cls = "faster" if verdict["faster"] else "slower"
        word = "faster" if verdict["faster"] else "slower"
        headline = f"{verdict['pct']:.0f}%"
        caption = (
            f"ghx is <strong>{word}</strong> than gh by this margin, weighted by the "
            f"median runtime of each scenario (time-weighted speedup "
            f"{verdict['weighted_speedup']:.2f}\u00d7)."
        )
        verdict_html = f"""
        <div class="card fade headline">
          <div class="big {cls}">{headline}</div>
          <div class="caption">{caption}</div>
        </div>
        <div class="card fade">
          <div class="stat-grid">
            <div class="stat"><div class="k">Scenarios won</div><div class="v">{verdict["wins"]} / {verdict["scored"]}</div></div>
            <div class="stat"><div class="k">Mean speedup</div><div class="v">{verdict["mean_speedup"]:.2f}\u00d7</div></div>
            <div class="stat"><div class="k">Time-weighted speedup</div><div class="v">{verdict["weighted_speedup"]:.2f}\u00d7</div></div>
            <div class="stat"><div class="k">ghx total (medians)</div><div class="v">{esc(fmt_time(verdict["total_ghx"]))}</div></div>
            <div class="stat"><div class="k">gh total (medians)</div><div class="v">{esc(fmt_time(verdict["total_gh"]))}</div></div>
            <div class="stat"><div class="k">{esc(verdict["gain_label"])}</div><div class="v">{esc(fmt_time(verdict["gain"]))}</div></div>
          </div>
        </div>"""
    else:
        verdict_html = """
        <div class="card fade headline" style="grid-column: 1 / -1;">
          <div class="big mixed">no data</div>
          <div class="caption">No scenario completed successfully for both tools. See the notes below.</div>
        </div>"""

    env_rows = "".join(
        f'<div class="row"><span class="k">{esc(k)}</span><span class="mono">{esc(v)}</span></div>'
        for k, v in ctx["env"].items()
    )

    table_rows = []
    for case in ctx["cases"]:
        if case["status"] == "ok":
            table_rows.append(
                f"""
        <tr>
          <td>{esc(case["label"])}</td>
          <td>{esc(case["cache"])}</td>
          <td>{esc(case["parity_label"])}</td>
          <td class="num" data-value="{case["ghx"]["median"]:.6f}">{esc(fmt_time(case["ghx"]["median"]))}</td>
          <td class="num" data-value="{case["gh"]["median"]:.6f}">{esc(fmt_time(case["gh"]["median"]))}</td>
          <td class="num" data-value="{case["speedup"]:.6f}">{case["speedup"]:.2f}\u00d7</td>
          <td class="num" data-value="{case["delta"]:.6f}">{esc(fmt_time(case["delta"]))}</td>
          <td class="num" data-value="{case["ghx"]["stdev"]:.6f}">{esc(fmt_time(case["ghx"]["stdev"]))}</td>
          <td class="num" data-value="{case["gh"]["stdev"]:.6f}">{esc(fmt_time(case["gh"]["stdev"]))}</td>
        </tr>"""
            )
        else:
            table_rows.append(
                f"""
        <tr>
          <td>{esc(case["label"])}</td>
          <td>{esc(case["cache"])}</td>
          <td>{esc(case["parity_label"])}</td>
          <td class="num" colspan="6" style="color: var(--muted);">skipped</td>
        </tr>"""
            )

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ghx vs gh benchmark</title>
<style>{CSS}</style>
</head>
<body>
<div class="wrap">
  <header class="hero fade">
    <h1>ghx vs gh \u2014 benchmark report</h1>
    <p>Wall-clock comparison of equivalent read commands across warm-cache, cold-cache and forced-refresh scenarios.</p>
    <div class="hero-meta">
      <span class="pill">repository <strong>{esc(ctx["repo"])}</strong></span>
      <span class="pill">ghx backend <strong>{esc(ctx["storage"])}</strong></span>
      <span class="pill">{ctx["runs"]} runs + {ctx["warmup"]} warmup</span>
      <span class="pill">generated {esc(ctx["generated_at"])}</span>
    </div>
  </header>

  <div class="toolbar"><button class="theme-toggle" id="theme-toggle" type="button">Dark mode</button></div>

  <section class="section">
    <h2>Verdict</h2>
    <div class="sub">Positive ratios mean ghx finished first. Time-weighted figures are dominated by the scenarios that take the longest. Not every scenario compares equal work: each card states how the two workloads differ.</div>
    <div class="verdict">{verdict_html}</div>
  </section>

  <section class="section">
    <h2>Speedup by scenario</h2>
    <div class="sub">Ratios come from median runtimes on a log scale. Green bars sit right of parity (ghx faster); red bars sit left of it (ghx slower).</div>
    <div class="card fade">{ctx["speedup_chart"] or '<div class="note">No successful scenarios to chart.</div>'}</div>
    {'<div class="note" style="margin-top:12px;">' + esc(ctx["chart_caveat"]) + "</div>" if ctx["chart_caveat"] else ""}
  </section>

  <section class="section">
    <h2>Scenario detail</h2>
    <div class="sub">Each card shows the median bars alongside the full sample distribution, plus a fairness note on how the two workloads differ.</div>
    <div class="cases">{"".join(render_case_card(c) for c in ctx["cases"])}</div>
  </section>

  <section class="section">
    <h2>All statistics</h2>
    <div class="sub">Click a column header to sort. Times are derived from {ctx["runs"]} samples per tool per scenario.</div>
    <div class="table-wrap fade">
      <table data-sortable>
        <thead>
          <tr>
            <th data-sort="text">Scenario</th>
            <th data-sort="text">Cache</th>
            <th data-sort="text">Work</th>
            <th class="num" data-sort="num">ghx median</th>
            <th class="num" data-sort="num">gh median</th>
            <th class="num" data-sort="num">Speedup</th>
            <th class="num" data-sort="num">ghx\u2212gh</th>
            <th class="num" data-sort="num">ghx stdev</th>
            <th class="num" data-sort="num">gh stdev</th>
          </tr>
        </thead>
        <tbody>{"".join(table_rows)}</tbody>
      </table>
    </div>
  </section>

  <section class="section">
    <h2>Environment</h2>
    <div class="sub">Factors that affect absolute timings and reproducibility.</div>
    <div class="card fade"><div class="env-grid">{env_rows}</div></div>
  </section>

  <section class="section">
    <h2>Methodology</h2>
    <div class="card fade">
      <ul style="margin: 0; padding-left: 18px; font-size: 0.88rem;">
        <li>ghx runs against a throwaway <code>--cache-dir</code>; the real <code>~/.cache/ghx</code> is never modified.</li>
        <li>Warm scenarios use a cache populated once with <code>ghx cache</code> before timing starts.</li>
        <li>Cold scenarios start each run from an empty cache directory, so ghx must fetch from the API like gh.</li>
        <li>Forced-refresh scenarios pass <code>--refresh</code> so both tools hit the API and only per-tool overhead differs.</li>
        <li>Runs are interleaved (ghx first on even iterations, gh first on odd) to reduce network drift bias.</li>
        <li>Warmup runs are discarded. Medians are reported because API latency is noisy and skewed.</li>
        <li>Output formats differ between the tools, so only runtime and success are compared, not byte-identical output.</li>
        <li>Every scenario carries a fairness badge. "Different work" means ghx serves from a local cache while gh calls the API. "ghx does more" means ghx builds a full local cache while gh fetches a single page. "ghx does less" means both call the API but ghx requests fewer fields or skips data gh fetches.</li>
        <li>In the forced-refresh view scenarios ghx does less work: gh issues a second query for project items and requests a richer field set (reactions, sub-issues, blocking relations, status-check rollup), none of which ghx fetches. Those timings show the cost of that extra work, not a like-for-like win.</li>
      </ul>
      <details>
        <summary>Raw results (JSON)</summary>
        <pre>{esc(ctx["raw_json"])}</pre>
      </details>
    </div>
  </section>

  <footer>
    Generated by <span class="mono">scripts/benchmark.py</span>. Timings are wall-clock and include process startup and network latency.
  </footer>
</div>
<script>{JS}</script>
</body>
</html>
"""


# ---------------------------------------------------------------------------
# Environment and orchestration
# ---------------------------------------------------------------------------


def tool_version(binary: Path, args: list[str]) -> str:
    try:
        proc = subprocess.run(
            [str(binary), *args],
            capture_output=True,
            text=True,
            errors="replace",
            timeout=15,
            check=False,
        )
        text = (proc.stdout or proc.stderr or "").strip().splitlines()
        return text[0] if text else "unknown"
    except (OSError, subprocess.SubprocessError):  # fmt: skip
        return "unknown"


def collect_env(cfg: Config) -> dict[str, str]:
    return {
        "Repository": cfg.repo,
        "ghx": tool_version(cfg.ghx_bin, ["--version"]),
        "gh": tool_version(cfg.gh_bin, ["--version"]),
        "ghx storage backend": cfg.storage,
        "ghx path": str(cfg.ghx_bin),
        "gh path": str(cfg.gh_bin),
        "OS": platform.platform(),
        "Architecture": platform.machine(),
        "CPU cores": str(os.cpu_count() or "unknown"),
        "Python": platform.python_version(),
        "Runs per tool": str(cfg.runs),
        "Warmups per tool": str(cfg.warmup),
        "Command timeout": f"{cfg.timeout:.0f}s",
    }


def resolve_binary(
    explicit: str | None, name: str, fallback_local: Path | None = None
) -> Path:
    if explicit:
        path = Path(explicit).expanduser()
        if path.exists():
            return path.resolve()
        found = shutil.which(explicit)
        if found:
            return Path(found)
        raise SystemExit(f"error: {name} binary not found at '{explicit}'")
    found = shutil.which(name)
    if found:
        return Path(found)
    if fallback_local and fallback_local.exists():
        return fallback_local
    raise SystemExit(
        f"error: '{name}' not found on PATH. Pass --{name} /path/to/{name}."
    )


def build_ghx() -> Path:
    """Build ghx from the repository root into a temp directory."""
    print("Building ghx from source...")
    out_dir = Path(tempfile.mkdtemp(prefix="ghx-bench-build-"))
    out = out_dir / "ghx"
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


def pick_numbers(
    gh_bin: Path, repo: str, timeout: float
) -> tuple[int | None, int | None]:
    """Find one issue and one PR number in the target repository."""
    issue_number: int | None = None
    pr_number: int | None = None
    try:
        issues = run_gh_json(
            gh_bin,
            [
                "issue",
                "list",
                "--repo",
                repo,
                "--state",
                "all",
                "--limit",
                "1",
                "--json",
                "number",
            ],
            timeout,
        )
        if isinstance(issues, list) and issues:
            issue_number = int(issues[0]["number"])
    except (RuntimeError, ValueError, KeyError, subprocess.SubprocessError):  # fmt: skip
        pass
    try:
        prs = run_gh_json(
            gh_bin,
            [
                "pr",
                "list",
                "--repo",
                repo,
                "--state",
                "all",
                "--limit",
                "1",
                "--json",
                "number",
            ],
            timeout,
        )
        if isinstance(prs, list) and prs:
            pr_number = int(prs[0]["number"])
    except (RuntimeError, ValueError, KeyError, subprocess.SubprocessError):  # fmt: skip
        pass
    return issue_number, pr_number


def print_summary(ctx: dict, output: Path) -> None:
    print()
    print(f"ghx backend: {ctx['storage']}")
    print(f"{'scenario':<42} {'ghx median':>12} {'gh median':>12} {'speedup':>16}")
    print("-" * 86)
    for case in ctx["cases"]:
        if case["status"] != "ok":
            print(f"{case['label']:<42} {'skipped':>12}")
            continue
        print(
            f"{case['label']:<42} {fmt_time(case['ghx']['median']):>12} "
            f"{fmt_time(case['gh']['median']):>12} {fmt_speedup(case['speedup']):>16}"
        )
    verdict = ctx["verdict"]
    print()
    if verdict.get("available"):
        word = "faster" if verdict["faster"] else "slower"
        print(
            f"Overall: ghx is {verdict['pct']:.0f}% {word} "
            f"(time-weighted {verdict['weighted_speedup']:.2f}x, "
            f"{verdict['wins']}/{verdict['scored']} scenarios won)."
        )
    else:
        print("Overall: no comparable data.")
    print(f"Report written to {output}")


def demo_results() -> list[CaseResult]:
    """Fabricate deterministic results so the report can be previewed offline."""
    repo = "octocat/hello-world"
    r = ["--repo", repo]
    specs = [
        (
            "issue-list-warm",
            "Issue list (warm cache)",
            "issue",
            "warm",
            [0.004, 0.005, 0.004, 0.006, 0.004],
            [0.31, 0.44, 0.38, 0.52, 0.35],
        ),
        (
            "pr-list-warm",
            "PR list (warm cache)",
            "pr",
            "warm",
            [0.006, 0.006, 0.007, 0.006, 0.008],
            [0.40, 0.46, 0.37, 0.58, 0.42],
        ),
        (
            "issue-list-cold",
            "Issue list (cold cache)",
            "issue",
            "cold",
            [0.62, 0.71, 0.68, 0.83, 0.66],
            [0.33, 0.41, 0.36, 0.49, 0.38],
        ),
        (
            "pr-list-cold",
            "PR list (cold cache)",
            "pr",
            "cold",
            [0.74, 0.88, 0.80, 0.97, 0.79],
            [0.39, 0.45, 0.36, 0.55, 0.41],
        ),
        (
            "issue-view-warm",
            "Issue view #1 (warm cache)",
            "issue",
            "warm",
            [0.003, 0.003, 0.004, 0.003, 0.003],
            [0.29, 0.35, 0.31, 0.44, 0.33],
        ),
        (
            "issue-view-refresh",
            "Issue view #1 (forced refresh)",
            "issue",
            "refresh",
            [0.22, 0.25, 0.23, 0.29, 0.24],
            [0.30, 0.36, 0.32, 0.42, 0.34],
        ),
        (
            "pr-view-warm",
            "PR view #9 (warm cache)",
            "pr",
            "warm",
            [0.004, 0.005, 0.004, 0.005, 0.004],
            [0.32, 0.38, 0.34, 0.47, 0.36],
        ),
        (
            "pr-view-refresh",
            "PR view #9 (forced refresh)",
            "pr",
            "refresh",
            [0.24, 0.27, 0.25, 0.31, 0.26],
            [0.33, 0.40, 0.35, 0.50, 0.37],
        ),
    ]
    parity_by_cache = {"warm": "different", "cold": "ghx-more", "refresh": "ghx-less"}
    notes = {
        "different": "ghx serves from the local cache; gh calls the API.",
        "ghx-more": "ghx builds a full local cache before filtering; gh fetches one page.",
        "ghx-less": "Both call the API, but ghx requests less data than gh.",
    }
    results: list[CaseResult] = []
    for key, label, category, cache, ghx_times, gh_times in specs:
        parity = parity_by_cache.get(cache, "different")
        case = BenchCase(
            key=key,
            label=label,
            description="Synthetic data for report preview.",
            category=category,
            cache=cache,
            ghx_args=["issue", "list", *r],
            gh_args=["issue", "list", *r],
            parity=parity,
            parity_note=notes[parity],
        )
        results.append(
            CaseResult(
                case=case,
                ghx=ToolResult(
                    tool="ghx", samples=[Sample(t, 0, 0) for t in ghx_times]
                ),
                gh=ToolResult(tool="gh", samples=[Sample(t, 0, 0) for t in gh_times]),
            )
        )
    return results


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Benchmark ghx against gh and write an HTML report.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--repo",
        help="Target repository as OWNER/REPO (default: detected from git remote)",
    )
    parser.add_argument(
        "--ghx", help="Path to the ghx binary (default: on PATH, then ./ghx)"
    )
    parser.add_argument("--gh", help="Path to the gh binary (default: on PATH)")
    parser.add_argument(
        "--storage",
        choices=("file", "sqlite"),
        default="sqlite",
        help="ghx cache backend to benchmark (passed to ghx as --storage)",
    )
    parser.add_argument(
        "--build",
        action="store_true",
        help="Build ghx from this repository before running",
    )
    parser.add_argument(
        "--runs", type=int, default=5, help="Timed runs per tool per scenario"
    )
    parser.add_argument(
        "--warmup",
        type=int,
        default=1,
        help="Discarded warmup runs per tool per scenario",
    )
    parser.add_argument(
        "--timeout", type=float, default=180.0, help="Per-command timeout in seconds"
    )
    parser.add_argument(
        "--cache-timeout",
        type=float,
        default=900.0,
        help="Timeout for the one-off warm cache population",
    )
    parser.add_argument(
        "--no-cold", action="store_true", help="Skip cold-cache scenarios"
    )
    parser.add_argument(
        "--no-refresh", action="store_true", help="Skip forced-refresh scenarios"
    )
    parser.add_argument("--only", help="Regex filter on scenario label")
    parser.add_argument(
        "-o", "--output", default=DEFAULT_OUTPUT, help="HTML report output path"
    )
    parser.add_argument(
        "--json", dest="json_path", help="Also write raw results as JSON"
    )
    parser.add_argument(
        "--demo",
        action="store_true",
        help="Render a synthetic report without running anything",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="Print the planned commands and exit"
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv if argv is not None else sys.argv[1:])

    if args.demo:
        cfg = Config(
            repo="octocat/hello-world",
            ghx_bin=Path("ghx"),
            gh_bin=Path("gh"),
            storage=args.storage,
            runs=5,
            warmup=1,
            timeout=args.timeout,
            cache_timeout=args.cache_timeout,
            work_dir=Path(tempfile.gettempdir()),
            warm_dir=Path(tempfile.gettempdir()) / "ghx-bench-warm",
            cold_root=Path(tempfile.gettempdir()) / "ghx-bench-cold",
            only=None,
        )
        env = {
            "Repository": cfg.repo,
            "ghx": "ghx version 0.0.0-demo",
            "gh": "gh version 2.101.0 (demo)",
            "ghx storage backend": cfg.storage,
            "OS": platform.platform(),
            "Architecture": platform.machine(),
            "CPU cores": str(os.cpu_count() or "unknown"),
            "Python": platform.python_version(),
            "Runs per tool": str(cfg.runs),
            "Warmups per tool": str(cfg.warmup),
        }
        ctx = build_context(demo_results(), cfg, env)
        output = Path(args.output)
        output.write_text(render_report(ctx), encoding="utf-8")
        print_summary(ctx, output)
        return 0

    repo = args.repo or detect_repo(Path.cwd())
    if not repo:
        print(
            "error: could not detect a repository; pass --repo OWNER/REPO",
            file=sys.stderr,
        )
        return 2

    ghx_bin = (
        build_ghx()
        if args.build
        else resolve_binary(args.ghx, "ghx", REPO_ROOT / "ghx")
    )
    gh_bin = resolve_binary(args.gh, "gh")

    work_dir = Path(tempfile.mkdtemp(prefix="ghx-bench-"))
    cfg = Config(
        repo=repo,
        ghx_bin=ghx_bin,
        gh_bin=gh_bin,
        storage=args.storage,
        runs=args.runs,
        warmup=args.warmup,
        timeout=args.timeout,
        cache_timeout=args.cache_timeout,
        work_dir=work_dir,
        warm_dir=work_dir / "warm",
        cold_root=work_dir / "cold",
        only=re.compile(args.only) if args.only else None,
    )
    cfg.warm_dir.mkdir(parents=True, exist_ok=True)
    cfg.cold_root.mkdir(parents=True, exist_ok=True)

    env = collect_env(cfg)
    print(f"Repository: {repo}")
    print(f"ghx:        {env['ghx']} ({ghx_bin})")
    print(f"ghx backend: {args.storage}")
    print(f"gh:         {env['gh']} ({gh_bin})")
    print(f"Runs:       {args.runs} timed + {args.warmup} warmup per tool per scenario")

    issue_number, pr_number = pick_numbers(gh_bin, repo, args.timeout)
    if issue_number is None:
        print("warning: no issue found; issue view scenarios will be skipped")
    if pr_number is None:
        print("warning: no PR found; pr view scenarios will be skipped")

    cases = make_cases(repo, issue_number, pr_number)
    if args.no_cold:
        cases = [c for c in cases if c.cache != "cold"]
    if args.no_refresh:
        cases = [c for c in cases if c.cache != "refresh"]
    if cfg.only:
        cases = [c for c in cases if cfg.only.search(c.label)]

    if args.dry_run:
        print("\nPlanned scenarios:")
        for case in cases:
            print(f"\n  {case.label}  [{case.cache}]")
            print(f"    ghx: {' '.join(build_cmd(case, 'ghx', cfg, 'timed', 0))}")
            print(f"    gh:  {' '.join(build_cmd(case, 'gh', cfg, 'timed', 0))}")
        shutil.rmtree(work_dir, ignore_errors=True)
        return 0

    if not cases:
        print("error: no scenarios selected", file=sys.stderr)
        shutil.rmtree(work_dir, ignore_errors=True)
        return 2

    warm_needed = any(c.cache in ("warm", "refresh") for c in cases)
    warm_ok = True
    warm_error = ""
    if warm_needed:
        print("\nPopulating warm cache (one-off)...")
        warm_ok, warm_error = populate_warm_cache(cfg)
        if warm_ok:
            print("Warm cache ready.")
        else:
            print(f"warning: warm cache population failed: {warm_error}")

    results: list[CaseResult] = []
    for case in cases:
        if case.cache in ("warm", "refresh") and not warm_ok:
            results.append(
                CaseResult(
                    case=case,
                    ghx=ToolResult(
                        tool="ghx", error=f"warm cache unavailable: {warm_error}"
                    ),
                    gh=ToolResult(
                        tool="gh", error=f"warm cache unavailable: {warm_error}"
                    ),
                )
            )
            print(f"  skipped  {case.label} (no warm cache)")
            continue
        print(f"  running  {case.label} ... ", end="", flush=True)
        result = run_case(case, cfg)
        results.append(result)
        if result.status == "ok":
            ghx_med = summarize(result.ghx.durations)["median"]
            gh_med = summarize(result.gh.durations)["median"]
            ratio = gh_med / ghx_med if ghx_med > 0 else 0.0
            print(
                f"ghx {fmt_time(ghx_med)} vs gh {fmt_time(gh_med)} ({fmt_speedup(ratio)})"
            )
        else:
            detail = result.ghx.error or result.gh.error or "incomplete"
            print(f"skipped ({detail[:80]})")

    ctx = build_context(results, cfg, env)
    output = Path(args.output)
    output.write_text(render_report(ctx), encoding="utf-8")

    if args.json_path:
        Path(args.json_path).write_text(ctx["raw_json"], encoding="utf-8")
        print(f"Raw JSON written to {args.json_path}")

    print_summary(ctx, output)
    shutil.rmtree(work_dir, ignore_errors=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
