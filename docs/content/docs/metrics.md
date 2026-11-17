---
title: Metrics
description: "Export gitlab-sync run, mirroring, GitLab API and git metrics to an OpenTelemetry collector over OTLP, and the full list of metrics, attributes and resource attributes."
weight: 45
---

gitlab-sync can push [OpenTelemetry](https://opentelemetry.io/) metrics to an OTLP collector (OpenTelemetry Collector, Grafana Alloy, Prometheus OTLP receiver, ...). Metrics give an overview of each run: how many groups, projects, releases and issues were synchronized, how long each phase took, how it ended, and how the GitLab APIs and git behaved. The logs keep the details of every group and project.

gitlab-sync runs once and exits, so the metrics are **pushed** rather than scraped: periodically during the run, then one last time before the process exits.

## Enabling metrics

Metrics are **disabled by default**. They are configured only through the standard [OpenTelemetry environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/); there is no command line flag.

Metrics are enabled as soon as one of these is set:

| Environment variable | Description |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Base URL of the collector, for example `http://otel-collector:4318`. For HTTP, `/v1/metrics` is appended. |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Full URL of the metrics endpoint. Takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT`. |
| `OTEL_METRICS_EXPORTER=otlp` | Enables the OTLP exporter with its default endpoint (`http://localhost:4318`). |

They stay disabled, whatever the endpoint, when `OTEL_SDK_DISABLED=true` or when `OTEL_METRICS_EXPORTER` is set to anything other than `otlp` (for example `none`).

The other standard variables are honoured too:

| Environment variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_PROTOCOL` / `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | `http/protobuf` | `http/protobuf` or `grpc`. Other values (such as `http/json`) log a warning and fall back to `http/protobuf`. |
| `OTEL_EXPORTER_OTLP_HEADERS` / `OTEL_EXPORTER_OTLP_METRICS_HEADERS` | | Extra headers, such as an authentication token: `Authorization=Bearer xxx`. |
| `OTEL_EXPORTER_OTLP_TIMEOUT` / `OTEL_EXPORTER_OTLP_METRICS_TIMEOUT` | `10000` | Timeout of one export, in milliseconds. |
| `OTEL_EXPORTER_OTLP_COMPRESSION` | | `gzip` to compress the payloads. |
| `OTEL_EXPORTER_OTLP_CERTIFICATE`, `..._CLIENT_CERTIFICATE`, `..._CLIENT_KEY` | | TLS CA and client certificate files. |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | Interval between two exports during the run, in milliseconds. |
| `OTEL_SERVICE_NAME` | `gitlab-sync` | Overrides the `service.name` resource attribute. |
| `OTEL_RESOURCE_ATTRIBUTES` | | Extra resource attributes, such as `deployment.environment.name=prod,service.instance.id=nightly`. |

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
gitlab-sync --mirror-mapping mapping.json ...
```

{{< callout type="info" >}}
Telemetry never changes the outcome of a run. When the collector cannot be reached, gitlab-sync logs a single warning (further errors are logged at debug level) and the exit code (`0`, `1` or `2`) stays the one of the mirroring. The final export is bounded to 10 seconds.
{{< /callout >}}

## Metrics

Metric names follow the OpenTelemetry conventions. Most backends translate them; Prometheus replaces the dots with underscores and appends the unit and the `_total` suffix for counters, as shown in the **Prometheus name** column.

### Run

Recorded once, at the end of the run.

| Metric | Prometheus name | Type | Unit | Description |
|---|---|---|---|---|
| `gitlab_sync.run.duration` | `gitlab_sync_run_duration_seconds` | Gauge | `s` | Duration of the last mirroring run. |
| `gitlab_sync.run.status` | `gitlab_sync_run_status` | Gauge | `{code}` | Exit code of the last run: `0` success, `1` blocking errors, `2` non-blocking errors only. |
| `gitlab_sync.phase.duration` | `gitlab_sync_phase_duration_seconds` | Gauge | `s` | Duration of each phase of the last run, by `phase`. |
| `gitlab_sync.errors` | `gitlab_sync_errors_total` | Counter | `{error}` | Errors reported during the run, by `severity`. |

These are gauges because every run is a separate process: they answer "how did the last run go". A run that is aborted by a fatal configuration error (such as an unreadable mirror mapping file) exits before telemetry is set up and records nothing.

The phases run one after the other:

| `phase` | What it covers |
|---|---|
| `fetch` | Connecting to both instances and fetching the groups and projects of the mirror mapping. |
| `groups` | Creating the destination groups. |
| `projects` | Mirroring the projects: git, attributes, avatar, CI/CD catalog, releases and issues. |
| `dry_run` | Printing the dry run output (`--dry-run` only, instead of `groups` and `projects`). |

### Mirrored objects

Counts every GitLab object the mirroring went through. Nothing is recorded during a dry run.

| Metric | Prometheus name | Type | Unit | Attributes | Description |
|---|---|---|---|---|---|
| `gitlab_sync.entities` | `gitlab_sync_entities_total` | Counter | `{entity}` | `entity`, `operation`, `outcome` | Groups, projects, releases, release assets and issues processed by the mirroring. |

What each `entity` and `operation` pair means:

| `entity` | `create` | `sync` | `skip` | `delete` |
|---|---|---|---|---|
| `group` | Group created on the destination. | | Group already exists, left as it is. | |
| `project` | Project created, then mirrored. | Existing project mirrored again (git, attributes, avatar, CI/CD catalog). | | |
| `release` | Release created. | Existing release whose assets were synchronized (`mirror_release_assets`). | Existing release, left as it is. | Release the source no longer has, deleted (`mirror_release_assets`). |
| `asset` | Release asset link created (and its file copied when needed). | Existing release asset checked, and its file copied again when it changed. | | Release asset the source release no longer has, deleted. |
| `issue` | Issue created. | | Issue with the same title already exists. | |

A project counts as a `failure` when its git mirroring, attributes, avatar or CI/CD catalog step failed. Its releases and issues are counted on their own.

### GitLab API

Recorded for every HTTP attempt both GitLab clients make, retries included.

| Metric | Prometheus name | Type | Unit | Attributes | Description |
|---|---|---|---|---|---|
| `gitlab_sync.api.requests` | `gitlab_sync_api_requests_total` | Counter | `{request}` | `role`, `method`, `status_class` | HTTP requests sent to the GitLab API, retries included. |
| `gitlab_sync.api.duration` | `gitlab_sync_api_duration_seconds` | Histogram | `s` | `role`, `method`, `status_class` | Duration of the HTTP requests sent to the GitLab API. |
| `gitlab_sync.api.retries` | `gitlab_sync_api_retries_total` | Counter | `{retry}` | `role` | Requests that were a retry of a failed attempt (see `--retry`). |

The `gitlab_sync.api.duration` histogram buckets are, in seconds: `0.005`, `0.01`, `0.025`, `0.05`, `0.075`, `0.1`, `0.25`, `0.5`, `0.75`, `1`, `2.5`, `5`, `7.5`, `10`, `30`, `60`.

### Git

Recorded for the freemium mirroring, where gitlab-sync clones the repositories and pushes them itself (see [Freemium destinations]({{< relref "freemium" >}})). Pull mirrors record nothing here.

| Metric | Prometheus name | Type | Unit | Attributes | Description |
|---|---|---|---|---|---|
| `gitlab_sync.git.push.duration` | `gitlab_sync_git_push_duration_seconds` | Histogram | `s` | `outcome` | Duration of each push to a destination repository. A push with nothing to update is a `success`. |
| `gitlab_sync.git.cache` | `gitlab_sync_git_cache_total` | Counter | `{lookup}` | `result` | Lookups of the repository cache (`--cache-dir` only). |

The `gitlab_sync.git.push.duration` histogram buckets are, in seconds: `0.1`, `0.25`, `0.5`, `1`, `2.5`, `5`, `10`, `30`, `60`, `120`, `300`, `600`, `1800`.

### Attributes

Attributes are kept low-cardinality on purpose: no group or project path is ever attached to a metric. Use the logs to find which project failed.

| Attribute | Values | Description |
|---|---|---|
| `phase` | `fetch`, `groups`, `projects`, `dry_run` | The phase of the run (see [Run](#run)). |
| `severity` | `blocking`, `non_blocking` | The severity of a reported error. Any blocking error makes the run exit with code `1`. |
| `entity` | `group`, `project`, `release`, `asset`, `issue` | The kind of GitLab object. |
| `operation` | `create`, `sync`, `skip`, `delete` | What the mirroring did with the object (see [Mirrored objects](#mirrored-objects)). |
| `outcome` | `success`, `failure` | Whether the operation, or the git push, succeeded. |
| `result` | `hit`, `miss`, `invalid` | `hit`: the cached repository was refreshed with a fetch. `miss`: it was not cached yet and was cloned. `invalid`: it was unusable (or could not be fetched) and was cloned again. |
| `role` | `source`, `destination` | The GitLab instance the request was sent to. |
| `method` | `GET`, `POST`, `PUT`, `DELETE`, ... | The HTTP request method. |
| `status_class` | `1xx`, `2xx`, `3xx`, `4xx`, `5xx`, `error`, `unknown` | The class of the response status. `error` means no response was received (DNS, connection or TLS error, timeout). |

### Resource attributes

Every metric carries these resource attributes, which identify the run (Prometheus exposes them on the `target_info` series, and uses `service.name` as the `job` label).

| Resource attribute | Example | Description |
|---|---|---|
| `service.name` | `gitlab-sync` | Overridable with `OTEL_SERVICE_NAME`. |
| `service.version` | `v1.2.3` | The gitlab-sync version. |
| `gitlab_sync.source.url` | `https://gitlab.com` | Source GitLab URL, without credentials, query or fragment. |
| `gitlab_sync.destination.url` | `https://gitlab.example.com` | Destination GitLab URL, without credentials, query or fragment. |
| `telemetry.sdk.*` | `opentelemetry`, `go`, `1.47.0` | The OpenTelemetry SDK in use. |

When several gitlab-sync jobs push to the same collector (one per mirror mapping, for example), give each its own identity so their series do not overwrite each other, for example with `OTEL_SERVICE_NAME=gitlab-sync-team-a` or `OTEL_RESOURCE_ATTRIBUTES=service.instance.id=team-a`.

## Example queries

Each run is a new process, so the counters start from zero at every run: the value of a counter is the total of the run that pushed it. A short run may push a single sample, which `rate()` and `increase()` cannot use, so read the last run's totals with `last_over_time()`. Use `rate()` for the API metrics of long runs, while they are in progress.

```promql
# Last run duration, and whether it succeeded
gitlab_sync_run_duration_seconds
gitlab_sync_run_status == 0

# Projects synchronized successfully by the last run, and the ones that failed
sum(last_over_time(gitlab_sync_entities_total{entity="project", outcome="success"}[1d]))
sum(last_over_time(gitlab_sync_entities_total{entity="project", outcome="failure"}[1d]))

# Objects processed by the last run, by kind and operation
sum by (entity, operation) (last_over_time(gitlab_sync_entities_total[1d]))

# Where the time went in the last run
last_over_time(gitlab_sync_phase_duration_seconds[1d])

# Blocking errors of the last run
sum(last_over_time(gitlab_sync_errors_total{severity="blocking"}[1d]))

# Cache hit ratio of the last run
sum(last_over_time(gitlab_sync_git_cache_total{result="hit"}[1d])) / sum(last_over_time(gitlab_sync_git_cache_total[1d]))

# GitLab API requests per second, by instance and status class
sum by (role, status_class) (rate(gitlab_sync_api_requests_total[5m]))

# 95th percentile of the API latency, by instance
histogram_quantile(0.95, sum by (role, le) (rate(gitlab_sync_api_duration_seconds_bucket[5m])))

# Share of retried requests
sum(rate(gitlab_sync_api_retries_total[5m])) / sum(rate(gitlab_sync_api_requests_total[5m]))
```

## Trying it locally

The repository ships a local stack with Grafana, Prometheus and an OTLP receiver ([grafana/otel-lgtm](https://github.com/grafana/docker-otel-lgtm)):

```bash
make otel-up    # Grafana http://localhost:3030, Prometheus http://localhost:9091
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_METRIC_EXPORT_INTERVAL=5000
gitlab-sync --mirror-mapping mapping.json ...
make otel-down
```

The host ports can be changed with `GRAFANA_PORT`, `PROMETHEUS_PORT`, `OTLP_HTTP_PORT` and `OTLP_GRPC_PORT`.
