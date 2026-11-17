---
title: Metrics
description: "Export gitlab-sync run and GitLab API metrics to an OpenTelemetry collector over OTLP, and the full list of metrics, attributes and resource attributes."
weight: 45
---

gitlab-sync can push [OpenTelemetry](https://opentelemetry.io/) metrics to an OTLP collector (OpenTelemetry Collector, Grafana Alloy, Prometheus OTLP receiver, ...). Metrics give an overview of each run: how long it took, how it ended, and how the GitLab APIs behaved. The logs keep the details of every group and project.

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

These are gauges because every run is a separate process: they answer "how did the last run go". A run that is aborted by a fatal configuration error (such as an unreadable mirror mapping file) exits before telemetry is set up and records nothing.

### GitLab API

Recorded for every HTTP attempt both GitLab clients make, retries included.

| Metric | Prometheus name | Type | Unit | Attributes | Description |
|---|---|---|---|---|---|
| `gitlab_sync.api.requests` | `gitlab_sync_api_requests_total` | Counter | `{request}` | `role`, `method`, `status_class` | HTTP requests sent to the GitLab API, retries included. |
| `gitlab_sync.api.duration` | `gitlab_sync_api_duration_seconds` | Histogram | `s` | `role`, `method`, `status_class` | Duration of the HTTP requests sent to the GitLab API. |
| `gitlab_sync.api.retries` | `gitlab_sync_api_retries_total` | Counter | `{retry}` | `role` | Requests that were a retry of a failed attempt (see `--retry`). |

The `gitlab_sync.api.duration` histogram buckets are, in seconds: `0.005`, `0.01`, `0.025`, `0.05`, `0.075`, `0.1`, `0.25`, `0.5`, `0.75`, `1`, `2.5`, `5`, `7.5`, `10`, `30`, `60`.

### Attributes

Attributes are kept low-cardinality on purpose: no group or project path is ever attached to a metric. Use the logs to find which project failed.

| Attribute | Values | Description |
|---|---|---|
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

```promql
# Last run duration, and whether it succeeded
gitlab_sync_run_duration_seconds
gitlab_sync_run_status == 0

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
