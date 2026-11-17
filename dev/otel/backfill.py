#!/usr/bin/env python3
"""Backfill the local Prometheus with simulated gitlab-sync runs.

Generates a few days of fake, but plausible, gitlab-sync metrics and loads
them into the Prometheus of the dev/otel stack, so the Grafana dashboard can
be developed without running hundreds of real mirroring runs.

The series match what the OTLP receiver produces from the real exporter
(same names, labels and cumulative-counter-per-process behaviour):

  gitlab_sync_run_duration_seconds   gauge, one sample at the end of each run
  gitlab_sync_run_status             gauge, exit code (0 OK, 1 blocking, 2 non-blocking)
  gitlab_sync_api_requests_total     counter {role, method, status, status_class}
  gitlab_sync_api_duration_seconds   classic histogram {role, method, status, status_class}
  gitlab_sync_api_retries_total      counter {role}
  gitlab_sync_entities_total         counter {entity, operation, outcome}
  gitlab_sync_errors_total           counter {severity}
  gitlab_sync_phase_duration_seconds gauge {phase}
  gitlab_sync_git_push_duration_seconds  classic histogram {outcome}
  gitlab_sync_git_cache_total        counter {result}
  target_info                        resource attributes (source/destination URLs)

It writes an OpenMetrics file, turns it into TSDB blocks with promtool inside
the otel-lgtm container, and moves the blocks into the Prometheus data
directory (they are picked up at the next compaction cycle, within a minute).

  make otel-backfill                         # 7 days, one run per hour
  make otel-backfill BACKFILL_ARGS=--fleet   # 8 tenants over 3 Kubernetes clusters
  dev/otel/backfill.py --days 3 --interval 15
  dev/otel/backfill.py --output /tmp/x.om    # only write the OpenMetrics file

Loading the same tenants twice over the same hours would count their runs twice,
so the script first asks Prometheus and refuses (--force to override). After a change
to the simulation, --replace deletes the simulated series first (real runs, with
other service versions, are kept).

Only the Python standard library is used.
"""

from __future__ import annotations

import argparse
import math
import os
import random
import shlex
import subprocess
import sys
import tempfile
import time
import json
import re
import urllib.error
import urllib.parse
import urllib.request
from collections import defaultdict
from dataclasses import dataclass, field

BUCKETS = [0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 30, 60]
GIT_BUCKETS = [0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800]

# (role, method) -> (share of the requests, median latency in seconds)
ENDPOINTS = {
    ("source", "GET"): (0.58, 0.07),
    ("destination", "GET"): (0.24, 0.09),
    ("destination", "POST"): (0.08, 0.35),
    ("destination", "PUT"): (0.08, 0.25),
    ("destination", "DELETE"): (0.02, 0.15),
}

EXPORT_INTERVAL = 60  # seconds, the OTEL_METRIC_EXPORT_INTERVAL default
CONTAINER_TMP = "/tmp/gitlab-sync-backfill"
PROM_DATA = "/data/prometheus"
PROMTOOL = "/otel-lgtm/prometheus/promtool"


@dataclass
class Run:
    start: float
    duration: float
    status: int
    version: str
    job: str = "gitlab-sync"
    # Kubernetes resource attributes (k8s_cluster_name, k8s_namespace_name), empty outside a fleet
    k8s: dict = field(default_factory=dict)
    # (role, method, status_class, status) -> request count
    requests: dict = field(default_factory=dict)
    # (role, method, status_class, status) -> (cumulative bucket counts, sum)
    latency: dict = field(default_factory=dict)
    # role -> retry count
    retries: dict = field(default_factory=dict)
    # (entity, operation, outcome) -> count
    entities: dict = field(default_factory=dict)
    # severity -> count
    errors: dict = field(default_factory=dict)
    # phase -> seconds
    phases: dict = field(default_factory=dict)
    # outcome -> (cumulative bucket counts, sum)
    pushes: dict = field(default_factory=dict)
    # result -> count
    cache: dict = field(default_factory=dict)

    @property
    def end(self) -> float:
        return self.start + self.duration

    def export_times(self) -> list[float]:
        """Timestamps of the periodic exports, plus the final flush at shutdown."""
        times = []
        t = self.start + EXPORT_INTERVAL
        while t < self.end:
            times.append(t)
            t += EXPORT_INTERVAL
        times.append(self.end)
        return times


@dataclass
class Tenant:
    """One gitlab-sync deployment: its identity, schedule, size and health."""
    job: str
    seed: object
    interval: float  # minutes between two runs
    requests: int  # mean API requests per run
    projects: int = 240
    k8s: dict = field(default_factory=dict)
    old_version: str = "v1.4.2"
    new_version: str = "v1.5.0"
    flaky: float = 0  # extra share of runs ending with warnings
    failing_hours: float = 0  # every run of the last N hours fails
    stopped_hours: float = 0  # no run during the last N hours (a stale tenant)
    incident: bool = True  # one destination incident in the history


def fleet_tenants(args: argparse.Namespace) -> list[Tenant]:
    """A fleet of gitlab-sync deployments spread over Kubernetes clusters and namespaces."""
    def tenant(cluster: str, namespace: str, **kw) -> Tenant:
        return Tenant(job=args.job, seed=f"{args.seed}/{cluster}/{namespace}",
                      k8s={"k8s_cluster_name": cluster, "k8s_namespace_name": namespace}, **kw)

    return [
        tenant("prod-eu-west-1", "platform", interval=60, requests=6000, projects=900),
        tenant("prod-eu-west-1", "payments", interval=30, requests=2500, projects=180, incident=False),
        tenant("prod-eu-west-1", "data", interval=60, requests=4000, projects=420, flaky=0.25),
        tenant("prod-us-east-1", "mobile", interval=120, requests=1500, projects=120),
        tenant("prod-us-east-1", "security", interval=60, requests=800, projects=60, failing_hours=10),
        tenant("prod-us-east-1", "web", interval=60, requests=3000, projects=300, new_version="v1.4.2"),
        tenant("staging", "platform", interval=15, requests=600, projects=90, new_version="v1.6.0-rc.1", incident=False),
        tenant("staging", "sandbox", interval=60, requests=300, projects=25, stopped_hours=40),
    ]


def default_tenant(args: argparse.Namespace) -> Tenant:
    return Tenant(job=args.job, seed=args.seed, interval=args.interval, requests=args.requests,
                  old_version=args.old_version, new_version=args.new_version)


def split_codes(status_class: str, method: str, count: int, busy: bool, incident: bool) -> dict:
    """Split a status class into plausible HTTP status codes (fixed shares: no random draw)."""
    if status_class == "error":
        shares = {"error": 1}
    elif status_class == "2xx":
        shares = {"POST": {"201": 1}, "DELETE": {"204": 1}}.get(method, {"200": 1})
    elif status_class == "5xx":
        shares = {"502": 0.5, "503": 0.4, "500": 0.1} if incident else {"500": 0.3, "502": 0.4, "503": 0.3}
    else:  # 4xx: lookups of missing objects, conflicts, permissions, and rate limiting when GitLab is busy
        rate_limited = 0.4 if incident else (0.15 if busy else 0.05)
        base = {"GET": {"404": 0.85, "403": 0.15}, "POST": {"409": 0.7, "400": 0.3},
                "PUT": {"404": 0.5, "409": 0.5}, "DELETE": {"404": 1}}[method]
        shares = {code: share * (1 - rate_limited) for code, share in base.items()} | {"429": rate_limited}
    codes = {code: int(count * share) for code, share in shares.items()}
    first = next(iter(codes))
    codes[first] += count - sum(codes.values())  # rounding leftovers
    return {code: n for code, n in codes.items() if n > 0}


def lognormal_cdf(x: float, median: float, sigma: float) -> float:
    return 0.5 * (1 + math.erf((math.log(x) - math.log(median)) / (sigma * math.sqrt(2))))


def histogram(count: int, median: float, sigma: float = 0.8, buckets: list[float] = BUCKETS) -> tuple[list[int], float]:
    """Cumulative bucket counts (last one is +Inf) and sum for count lognormal samples."""
    cumulative = [round(count * lognormal_cdf(b, median, sigma)) for b in buckets]
    cumulative.append(count)
    mean = median * math.exp(sigma**2 / 2)
    return cumulative, count * mean


def simulate_mirroring(run: Run, rng: random.Random, incident: bool, size: int = 240) -> None:
    """Mirrored objects, errors, phases and git metrics consistent with the run status."""
    def put(entity: str, operation: str, outcome: str, count: int) -> None:
        if count > 0:
            run.entities[(entity, operation, outcome)] = count

    scale = size / 240
    projects = size + rng.randint(-5, 5)
    created = rng.choice([0, 0, 0, 1, 2])
    failed = rng.randint(4, 15) if incident else (rng.randint(1, 3) if run.status != 0 else 0)
    put("group", "skip", "success", int(scale * (38 + rng.randint(0, 2))))
    put("group", "create", "success", rng.choice([0, 0, 0, 0, 1]))
    put("project", "create", "success", created)
    put("project", "sync", "success", projects - failed)
    put("project", "sync", "failure", failed)
    put("release", "skip", "success", int(scale * (1150 + rng.randint(0, 80))))
    put("release", "create", "success", rng.choice([0, 0, 1, 2, 3, 5]))
    put("release", "create", "failure", rng.randint(1, 3) if incident else 0)
    put("release", "sync", "success", int(scale * (rng.randint(20, 40))))
    put("release", "delete", "success", rng.choice([0, 0, 0, 0, 1]))
    put("asset", "sync", "success", int(scale * (300 + rng.randint(0, 30))))
    put("asset", "create", "success", rng.choice([0, 0, 2, 4, 8]))
    put("asset", "delete", "success", rng.choice([0, 0, 0, 1]))
    put("issue", "skip", "success", int(scale * (780 + rng.randint(0, 60))))
    put("issue", "create", "success", rng.randint(0, 20))
    put("issue", "create", "failure", rng.randint(0, 2) if run.status != 0 else 0)

    if run.status == 1:
        run.errors["blocking"] = rng.randint(1, 4) + (failed if incident else 0)
    if run.status != 0:
        run.errors["non_blocking"] = rng.randint(1, 6)

    fetch = run.duration * rng.uniform(0.08, 0.12)
    groups = run.duration * rng.uniform(0.02, 0.04)
    run.phases = {"fetch": fetch, "groups": groups, "projects": run.duration - fetch - groups}

    # Freemium mirroring: one push per project, mostly "already up to date".
    pushes_failed = failed // 2
    run.pushes["success"] = histogram(projects + created - pushes_failed, 1.5 * (2 if incident else 1), 1.0, GIT_BUCKETS)
    if pushes_failed:
        run.pushes["failure"] = histogram(pushes_failed, 30, 0.8, GIT_BUCKETS)

    invalid = rng.choice([0, 0, 0, 1])
    run.cache = {"hit": projects - invalid, "miss": created, "invalid": invalid}
    run.cache = {k: v for k, v in run.cache.items() if v > 0}


def simulate(args: argparse.Namespace, tenant: Tenant) -> list[Run]:
    rng = random.Random(tenant.seed)
    end = time.time() - args.end_offset * 3600
    start = end - args.days * 86400
    step = tenant.interval * 60
    last_start = end - tenant.stopped_hours * 3600
    failing_from = end - tenant.failing_hours * 3600 if tenant.failing_hours else math.inf

    # One incident window: the destination answers 5xx, runs retry a lot and fail.
    incident_start = start + rng.uniform(0.35, 0.65) * (end - start)
    incident_end = incident_start + rng.uniform(3, 6) * 3600
    # Halfway through, a new version is rolled out and runs get ~20% faster.
    upgrade_at = start + 0.5 * (end - start)

    runs = []
    t = start
    while t < last_start:
        run_start = t + rng.uniform(0, 20)  # cron jitter
        hour = time.localtime(run_start).tm_hour
        busy = 1.3 if 8 <= hour < 19 else 1.0  # slower API during office hours
        upgraded = run_start >= upgrade_at
        incident = tenant.incident and incident_start <= run_start < incident_end
        version = tenant.new_version if upgraded else tenant.old_version

        latency_factor = busy * (0.8 if upgraded else 1.0) * (2.5 if incident else 1.0)
        n_requests = int(rng.gauss(tenant.requests, tenant.requests * 0.08))
        err_5xx = rng.uniform(0.25, 0.4) if incident else rng.uniform(0, 0.003)
        err_4xx = rng.uniform(0.01, 0.04)
        err_transport = 0.02 if incident else rng.choice([0, 0, 0, 0.0005])

        run = Run(start=run_start, duration=0, status=0, version=version, job=tenant.job, k8s=tenant.k8s)
        total_seconds = 0.0
        for (role, method), (share, median) in ENDPOINTS.items():
            n = int(n_requests * share * rng.uniform(0.9, 1.1))
            dst_5xx = err_5xx if role == "destination" else err_5xx * 0.05
            classes = {
                "4xx": int(n * err_4xx) if method == "GET" else int(n * err_4xx * 0.2),
                "5xx": int(n * dst_5xx),
                "error": int(n * err_transport) if role == "destination" else 0,
            }
            classes["2xx"] = n - sum(classes.values())
            for status_class, count in classes.items():
                if count <= 0:
                    continue
                med = median * latency_factor
                if status_class == "5xx":
                    med *= 3
                elif status_class == "error":
                    med = 30  # transport timeouts
                for code, n in split_codes(status_class, method, count, busy > 1, incident).items():
                    key = (role, method, status_class, code)
                    run.requests[key] = n
                    run.latency[key] = histogram(n, med)
                    total_seconds += run.latency[key][1]

        for role in ("source", "destination"):
            # retryablehttp retries 429, 5xx and connection errors.
            failed = sum(c for (r, _, sc, code), c in run.requests.items() if r == role and (sc in ("5xx", "error") or code == "429"))
            run.retries[role] = int(failed * rng.uniform(0.8, 1.0))

        # Requests run with some concurrency, plus git work not seen by the API metrics.
        run.duration = total_seconds / args.concurrency + rng.uniform(15, 40)
        roll = rng.random()
        if incident and roll < 0.7 or run_start >= failing_from:
            run.status = 1
        elif roll < 0.08 + tenant.flaky:
            run.status = 2
        elif roll < 0.1 + tenant.flaky:
            run.status = 1
        # Own random stream: adding metrics must not change the series generated before.
        mirroring_seed = tenant.seed * 100_003 + len(runs) if isinstance(tenant.seed, int) else f"{tenant.seed}/{len(runs)}"
        simulate_mirroring(run, random.Random(mirroring_seed), incident or run_start >= failing_from, tenant.projects)
        runs.append(run)
        t += step

    return runs


def fmt_labels(labels: dict) -> str:
    inner = ",".join(f'{k}="{v}"' for k, v in labels.items())
    return "{" + inner + "}"


def fmt_le(b: float) -> str:
    return "+Inf" if b == math.inf else (str(int(b)) if float(b).is_integer() else repr(b))


def render(runs: list[Run], args: argparse.Namespace) -> str:
    resource = lambda run: {"job": run.job, "service_name": run.job, "service_version": run.version, **run.k8s}  # noqa: E731
    out: list[str] = []

    # Gauges: recorded once, right before the shutdown flush.
    out.append("# HELP gitlab_sync_run_duration_seconds Duration of the last mirroring run.")
    out.append("# TYPE gitlab_sync_run_duration_seconds gauge")
    out.append("# UNIT gitlab_sync_run_duration_seconds seconds")
    for run in runs:
        out.append(f"gitlab_sync_run_duration_seconds{fmt_labels(resource(run))} {run.duration:.3f} {run.end:.3f}")

    out.append("# HELP gitlab_sync_run_status Exit code of the last mirroring run.")
    out.append("# TYPE gitlab_sync_run_status gauge")
    for run in runs:
        out.append(f"gitlab_sync_run_status{fmt_labels(resource(run))} {run.status} {run.end:.3f}")

    # target_info: one point per export, like the OTLP receiver does.
    out.append("# HELP target_info Target metadata")
    out.append("# TYPE target_info gauge")
    by_series: dict = defaultdict(list)
    for run in runs:
        labels = {
            **resource(run),
            "gitlab_sync_source_url": args.source_url,
            "gitlab_sync_destination_url": args.destination_url,
            "telemetry_sdk_language": "go",
            "telemetry_sdk_name": "opentelemetry",
        }
        for ts in run.export_times():
            by_series[fmt_labels(labels)].append(f"target_info{fmt_labels(labels)} 1 {ts:.3f}")
    for lines in by_series.values():
        out.extend(lines)

    # Counters and the histogram are cumulative per process: they restart from 0
    # every run and grow linearly through it.
    def progress(run: Run, ts: float) -> float:
        return min(1.0, (ts - run.start) / run.duration)

    out.append("# HELP gitlab_sync_api_requests HTTP requests sent to the GitLab API, retries included.")
    out.append("# TYPE gitlab_sync_api_requests counter")
    by_series = defaultdict(list)
    for run in runs:
        for (role, method, sc, code), count in run.requests.items():
            labels = fmt_labels({**resource(run), "method": method, "role": role, "status": code, "status_class": sc})
            for ts in run.export_times():
                by_series[labels].append(f"gitlab_sync_api_requests_total{labels} {round(count * progress(run, ts))} {ts:.3f}")
    for lines in by_series.values():
        out.extend(lines)

    out.append("# HELP gitlab_sync_api_retries HTTP requests to the GitLab API that were retries of a failed attempt.")
    out.append("# TYPE gitlab_sync_api_retries counter")
    by_series = defaultdict(list)
    for run in runs:
        for role, count in run.retries.items():
            labels = fmt_labels({**resource(run), "role": role})
            for ts in run.export_times():
                by_series[labels].append(f"gitlab_sync_api_retries_total{labels} {round(count * progress(run, ts))} {ts:.3f}")
    for lines in by_series.values():
        out.extend(lines)

    # The histogram is written as three untyped families: for a "histogram"
    # family the OpenMetrics parser rewrites le="1" as le="1.0", while the OTLP
    # receiver stores le="1", and mixed spellings would split the buckets.
    families: dict = {"bucket": defaultdict(list), "count": defaultdict(list), "sum": defaultdict(list)}
    for run in runs:
        for (role, method, sc, code), (cumulative, total) in run.latency.items():
            base = {**resource(run), "method": method, "role": role, "status": code, "status_class": sc}
            key = fmt_labels(base)
            for ts in run.export_times():
                p = progress(run, ts)
                for b, c in zip(BUCKETS + [math.inf], cumulative):
                    labels = fmt_labels({**base, "le": fmt_le(b)})
                    families["bucket"][labels].append(f"gitlab_sync_api_duration_seconds_bucket{labels} {round(c * p)} {ts:.3f}")
                families["count"][key].append(f"gitlab_sync_api_duration_seconds_count{key} {round(cumulative[-1] * p)} {ts:.3f}")
                families["sum"][key].append(f"gitlab_sync_api_duration_seconds_sum{key} {total * p:.3f} {ts:.3f}")
    for suffix, series in families.items():
        out.append(f"# TYPE gitlab_sync_api_duration_seconds_{suffix} unknown")
        for lines in series.values():
            out.extend(lines)

    def counter(family: str, help_text: str, values) -> None:
        """values(run) -> {labels dict as tuple of pairs: final count}"""
        out.append(f"# HELP {family} {help_text}")
        out.append(f"# TYPE {family} counter")
        series: dict = defaultdict(list)
        for run in runs:
            for pairs, count in values(run).items():
                labels = fmt_labels({**resource(run), **dict(pairs)})
                for ts in run.export_times():
                    series[labels].append(f"{family}_total{labels} {round(count * progress(run, ts))} {ts:.3f}")
        for lines in series.values():
            out.extend(lines)

    counter("gitlab_sync_entities", "Groups, projects, releases, release assets and issues processed by the mirroring.",
            lambda run: {(("entity", e), ("operation", o), ("outcome", r)): c for (e, o, r), c in run.entities.items()})
    counter("gitlab_sync_errors", "Errors reported during the run.",
            lambda run: {(("severity", s),): c for s, c in run.errors.items()})
    counter("gitlab_sync_git_cache", "Lookups of the repository cache.",
            lambda run: {(("result", r),): c for r, c in run.cache.items()})

    # Phase gauges: recorded at the end of each phase, then re-exported by every collection.
    out.append("# HELP gitlab_sync_phase_duration_seconds Duration of each phase of the last run.")
    out.append("# TYPE gitlab_sync_phase_duration_seconds gauge")
    series = defaultdict(list)
    for run in runs:
        ended = run.start
        for phase, seconds in run.phases.items():
            ended += seconds
            labels = fmt_labels({**resource(run), "phase": phase})
            for ts in run.export_times():
                if ts >= ended - 0.001:
                    series[labels].append(f"gitlab_sync_phase_duration_seconds{labels} {seconds:.3f} {ts:.3f}")
    for lines in series.values():
        out.extend(lines)

    # Git push histogram, untyped for the same le reason as the API one.
    families = {"bucket": defaultdict(list), "count": defaultdict(list), "sum": defaultdict(list)}
    for run in runs:
        for outcome, (cumulative, total) in run.pushes.items():
            base = {**resource(run), "outcome": outcome}
            key = fmt_labels(base)
            for ts in run.export_times():
                p = progress(run, ts)
                for b, c in zip(GIT_BUCKETS + [math.inf], cumulative):
                    labels = fmt_labels({**base, "le": fmt_le(b)})
                    families["bucket"][labels].append(f"gitlab_sync_git_push_duration_seconds_bucket{labels} {round(c * p)} {ts:.3f}")
                families["count"][key].append(f"gitlab_sync_git_push_duration_seconds_count{key} {round(cumulative[-1] * p)} {ts:.3f}")
                families["sum"][key].append(f"gitlab_sync_git_push_duration_seconds_sum{key} {total * p:.3f} {ts:.3f}")
    for suffix, fam in families.items():
        out.append(f"# TYPE gitlab_sync_git_push_duration_seconds_{suffix} unknown")
        for lines in fam.values():
            out.extend(lines)

    out.append("# EOF")
    return "\n".join(out) + "\n"


def sh(cmd: list[str]) -> None:
    print("+ " + shlex.join(cmd), file=sys.stderr)
    subprocess.run(cmd, check=True)


def replace(runs: list[Run], args: argparse.Namespace) -> None:
    """Delete the simulated series of these tenants (needs the Prometheus admin API).

    Only the versions the simulation uses are matched, so real runs of the same
    tenant (service.version "dev", for example) are kept.
    """
    tenants = {(r.job, tuple(sorted(r.k8s.items()))) for r in runs}
    # Regex-escaped, then escaped again for the PromQL string literal.
    versions = "|".join(sorted({re.escape(r.version) for r in runs})).replace("\\", "\\\\")
    for job, k8s in sorted(tenants):
        k8s = dict(k8s) or {"k8s_cluster_name": "", "k8s_namespace_name": ""}
        selector = fmt_labels({"job": job, **k8s})[:-1] + f',service_version=~"{versions}",__name__=~"gitlab_sync_.+|target_info"}}'
        url = f"{args.prometheus}/api/v1/admin/tsdb/delete_series?" + urllib.parse.urlencode({"match[]": selector})
        print(f"Deleting {selector}", file=sys.stderr)
        try:
            urllib.request.urlopen(urllib.request.Request(url, method="POST"), timeout=60).close()
        except urllib.error.HTTPError as err:
            if err.code in (404, 405):
                sys.exit("The Prometheus admin API is disabled: recreate the stack with `make otel-up` (dev/otel/compose.yaml enables it).")
            raise
    urllib.request.urlopen(urllib.request.Request(f"{args.prometheus}/api/v1/admin/tsdb/clean_tombstones", method="POST"), timeout=300).close()


def already_loaded(runs: list[Run], args: argparse.Namespace) -> list[str]:
    """Tenants that already have runs in the time window: loading again would count them twice."""
    tenants = {(r.job, tuple(sorted(r.k8s.items()))) for r in runs}
    start, end = min(r.start for r in runs), max(r.end for r in runs)
    found = []
    for job, k8s in sorted(tenants):
        # An unlabelled tenant must not match the fleet ones: pin the k8s labels to "" then.
        k8s = dict(k8s) or {"k8s_cluster_name": "", "k8s_namespace_name": ""}
        selector = fmt_labels({"job": job, **k8s})
        query = f"count(count_over_time(gitlab_sync_run_status{selector}[{int(end - start) + 1}s]))"
        url = f"{args.prometheus}/api/v1/query?" + urllib.parse.urlencode({"query": query, "time": f"{end:.3f}"})
        try:
            with urllib.request.urlopen(url, timeout=10) as resp:
                result = json.load(resp)["data"]["result"]
        except (OSError, ValueError, KeyError) as err:
            sys.exit(f"Cannot check {args.prometheus} for existing data ({err}); pass --force to load anyway.")
        if result:
            found.append(selector)
    return found


def load(path: str, args: argparse.Namespace) -> None:
    engine, container = args.engine, args.container
    blocks = f"{CONTAINER_TMP}/blocks"
    sh([engine, "exec", container, "rm", "-rf", CONTAINER_TMP])
    sh([engine, "exec", container, "mkdir", "-p", blocks])
    sh([engine, "cp", path, f"{container}:{CONTAINER_TMP}/data.om"])
    sh([engine, "exec", container, PROMTOOL, "tsdb", "create-blocks-from", "openmetrics", "--max-block-duration=24h",
        f"{CONTAINER_TMP}/data.om", blocks])
    sh([engine, "exec", container, "sh", "-c", f"mv {blocks}/* {PROM_DATA}/ && rm -rf {CONTAINER_TMP}"])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--days", type=float, default=7, help="history to generate (default: 7, keep it under the 15d retention)")
    parser.add_argument("--interval", type=float, default=60, help="minutes between two runs (default: 60)")
    parser.add_argument("--end-offset", type=float, default=1, help="hours before now the history ends (default: 1)")
    parser.add_argument("--requests", type=int, default=2500, help="mean API requests per run (default: 2500)")
    parser.add_argument("--concurrency", type=float, default=6, help="parallel API calls, scales the run duration (default: 6)")
    parser.add_argument("--job", default="gitlab-sync", help="service.name / job label (default: gitlab-sync)")
    parser.add_argument("--old-version", default="v1.4.2", help="service.version in the first half")
    parser.add_argument("--new-version", default="v1.5.0", help="service.version in the second half")
    parser.add_argument("--source-url", default="https://gitlab.source.example")
    parser.add_argument("--destination-url", default="https://gitlab.destination.example")
    parser.add_argument("--seed", type=int, default=42, help="random seed, for reproducible data")
    parser.add_argument("--engine", default=os.environ.get("DOCKER_ENGINE", "docker"), help="container engine (default: $DOCKER_ENGINE or docker)")
    parser.add_argument("--container", default="gitlab-sync-otel-otel-lgtm-1", help="otel-lgtm container name")
    parser.add_argument("--output", help="only write the OpenMetrics file there, do not load it")
    parser.add_argument("--fleet", action="store_true",
                        help="generate a fleet of 8 tenants over 3 Kubernetes clusters (k8s.cluster.name / k8s.namespace.name) instead of one deployment")
    parser.add_argument("--prometheus", default=f"http://localhost:{os.environ.get('PROMETHEUS_PORT', '9091')}",
                        help="Prometheus checked for already loaded data (default: http://localhost:$PROMETHEUS_PORT or 9091)")
    parser.add_argument("--force", action="store_true", help="load even when the same tenants already have data in the time window")
    parser.add_argument("--replace", action="store_true",
                        help="first delete the simulated series of the same tenants (any time), e.g. after changing the simulation")
    args = parser.parse_args()

    tenants = fleet_tenants(args) if args.fleet else [default_tenant(args)]
    runs = [run for tenant in tenants for run in simulate(args, tenant)]
    data = render(runs, args)
    failed = sum(r.status == 1 for r in runs)
    print(f"Generated {len(runs)} runs ({failed} failed) of {len(tenants)} tenant(s) over {args.days:g} days, "
          f"{len(data) // 1024} KiB", file=sys.stderr)

    if args.output:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(data)
        return 0

    if args.replace:
        replace(runs, args)
    if not args.force:
        loaded = already_loaded(runs, args)
        if loaded:
            print("These tenants already have runs in the time window; loading them again would count every run twice:",
                  *loaded, "Pass --replace to swap them for the new simulation, or --force.", sep="\n  ", file=sys.stderr)
            return 1

    with tempfile.NamedTemporaryFile("w", suffix=".om", delete=False, encoding="utf-8") as f:
        f.write(data)
    try:
        load(f.name, args)
    finally:
        os.unlink(f.name)
    print("Done: Prometheus loads the new blocks at its next compaction cycle (within a minute).", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
