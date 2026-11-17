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
| `gitlab_sync.api.requests` | `gitlab_sync_api_requests_total` | Counter | `{request}` | `role`, `method`, `status`, `status_class` | HTTP requests sent to the GitLab API, retries included. |
| `gitlab_sync.api.duration` | `gitlab_sync_api_duration_seconds` | Histogram | `s` | `role`, `method`, `status`, `status_class` | Duration of the HTTP requests sent to the GitLab API. |
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
| `status` | `200`, `201`, `204`, `304`, `401`, `403`, `404`, `409`, `429`, `500`, `502`, `503`, ..., `error` | The HTTP response status code. `error` means no response was received (DNS, connection or TLS error, timeout). |
| `status_class` | `1xx`, `2xx`, `3xx`, `4xx`, `5xx`, `error`, `unknown` | The class of the response status, for grouping the codes. `error` means no response was received, like `status`. |

### Resource attributes

Every metric carries these resource attributes, which identify the run (Prometheus exposes them on the `target_info` series, and uses `service.name` as the `job` label).

| Resource attribute | Example | Description |
|---|---|---|
| `service.name` | `gitlab-sync` | Overridable with `OTEL_SERVICE_NAME`. |
| `service.version` | `v1.2.3` | The gitlab-sync version. |
| `gitlab_sync.source.url` | `https://gitlab.com` | Source GitLab URL, without credentials, query or fragment. |
| `gitlab_sync.destination.url` | `https://gitlab.example.com` | Destination GitLab URL, without credentials, query or fragment. |
| `telemetry.sdk.*` | `opentelemetry`, `go`, `1.47.0` | The OpenTelemetry SDK in use. |

When several gitlab-sync jobs push to the same collector (one per mirror mapping, for example), give each its own identity so their series do not overwrite each other, for example with `OTEL_SERVICE_NAME=gitlab-sync-team-a` or `OTEL_RESOURCE_ATTRIBUTES=service.instance.id=team-a`. On Kubernetes, use the cluster and namespace (see [Several deployments](#several-deployments)).

## Several deployments

When gitlab-sync runs in many places (for example one CronJob per team namespace, across several clusters), each deployment is a **tenant**, identified by its cluster, its namespace and its `service.name` (the `job` label). The [fleet dashboard](#fleet-dashboard) shows all of them on one page.

Two things are needed:

1. **Each deployment sets its cluster and namespace** as resource attributes. In a Kubernetes CronJob, take the namespace from the downward API (`POD_NAMESPACE` must be declared before the variable that uses it):

   ```yaml
   env:
     - name: POD_NAMESPACE
       valueFrom:
         fieldRef:
           fieldPath: metadata.namespace
     - name: OTEL_EXPORTER_OTLP_ENDPOINT
       value: http://otel-collector.observability:4318
     - name: OTEL_RESOURCE_ATTRIBUTES
       value: k8s.cluster.name=prod-eu-west-1,k8s.namespace.name=$(POD_NAMESPACE)
   ```

   A collector running the [`k8sattributes` processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/k8sattributesprocessor) can add the same attributes instead.

2. **Prometheus turns these attributes into labels.** By default, Prometheus keeps resource attributes on `target_info` only, so list them in its OTLP configuration:

   ```yaml
   otlp:
     promote_resource_attributes:
       - k8s.cluster.name
       - k8s.namespace.name
   ```

   The series then carry `k8s_cluster_name` and `k8s_namespace_name` labels. The local stack already promotes them.

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

# Rate-limited GitLab API requests (429), by instance
sum by (role) (rate(gitlab_sync_api_requests_total{status="429"}[5m]))

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

### Grafana dashboards

Two ready-made dashboards live in [`dev/otel/dashboards/`](https://github.com/BoxBoxJason/gitlab-sync/tree/main/dev/otel/dashboards):

| Dashboard | File | Local URL | For |
|---|---|---|---|
| gitlab-sync | `gitlab-sync.json` | http://localhost:3030/d/gitlab-sync | Everything about one deployment. |
| gitlab-sync fleet | `gitlab-sync-fleet.json` | http://localhost:3030/d/gitlab-sync-fleet | An overview of [several deployments](#several-deployments). |

Load them, or update them after a change, into the local Grafana:

```bash
make otel-dashboard   # loads every dev/otel/dashboards/*.json
```

The target uses the Grafana dashboard API (`dashboard.grafana.app/v2`), so it needs a Grafana recent enough to serve it, like the one in the local stack. Set `GRAFANA_URL` to load them into another Grafana.

#### gitlab-sync dashboard

{{< image "images/dashboard-gitlab-sync.png" "The Overview tab of the gitlab-sync dashboard, on a week of simulated runs: last run status and duration, success rate, projects mirrored, errors, GitLab API totals, and the run status timeline." >}}

The Overview tab, on a week of [simulated runs](#simulated-data).

| Tab | Sub-tabs | Shows |
|---|---|---|
| Overview | | The last run: status, duration, projects mirrored and failed, blocking and non-blocking errors. Also the number of runs and their success rate, and the GitLab API totals: requests, error rate, retries, p95 latency. |
| Runs | Status, Duration, Versions | Run status and duration over time, failed runs per day, the fastest, average, p95 and slowest runs, and the run duration by gitlab-sync version. |
| Mirroring | Objects, Failures, Phases | [Mirrored objects](#mirrored-objects) per run, by kind and by operation, objects created and deleted per run, and a totals table (`entity` × `operation` × `outcome`). Failed objects and errors per run, and the failure ratio by kind. Phase durations stacked per run, the average and slowest phase, and each phase's share of the run time. |
| GitLab API | Traffic, Latency, Errors, Retries | Requests per run by instance, method and status class, requests by status code, throughput, latency percentiles and heatmap, failed requests per run and in a table by status code, retries, and rate-limited (`429`) requests. |
| Git | Pushes, Cache | [Git](#git) pushes per run by outcome, push p50/p95/p99 and heatmap, push time per run. Cache lookups per run by result, hit ratio, and invalid entries. |

Its variables filter the panels: `Data source`, `Cluster`, `Namespace`, `Job` (the `service.name`), `Version`, `Instance` (`role`), `Object` (`entity`, on the Mirroring tab), `Status` (the HTTP status code of the API calls), `Method` and `Run window`. `Cluster` and `Namespace` set to All also match deployments without Kubernetes attributes. The **Fleet** link in the header opens the fleet dashboard.

#### Fleet dashboard

A single page giving an overview of every tenant (cluster × namespace × job). It needs the `k8s.cluster.name` and `k8s.namespace.name` attributes described in [Several deployments](#several-deployments). Deployments without them are left out.

- **Stats:**
  - The number of tenants, split into healthy (last run `0`), warnings (last run `2`), failing (last run `1`) and stale (no run for longer than `Stale after`).
  - The number of runs, the success rate, and the projects mirrored (the sum of each tenant's last run).
- **Tenants table:** one row per tenant, failing tenants first, then stale ones, then the ones with warnings.
  - Columns: cluster, namespace, version, last run, time since the last run, duration, runs, success rate, projects, failed projects, blocking errors of the last run, API errors, API p95 latency, and the share of rate-limited (`429`) API requests.
  - Clicking a namespace opens the gitlab-sync dashboard filtered on that tenant.
- **Graphs:** one series per tenant for the run status timeline, run duration, success rate, projects mirrored per run, blocking errors per run, API error ratio, rate-limited (`429`) requests per run and API p95 latency.

Its variables are `Data source`, `Cluster`, `Namespace`, `Job`, `Run window` and `Stale after`.

### Simulated data

To work on the dashboard without running hundreds of real mirrorings, load a few days of simulated, but realistic, runs into the local Prometheus:

```bash
make otel-backfill                                   # 7 days, one run per hour
make otel-backfill BACKFILL_ARGS="--days 3 --interval 15"
./dev/otel/backfill.py --help                        # all the options
```

The script only needs Python 3. It writes the series in the OpenMetrics format, converts them with `promtool` inside the stack container, and Prometheus picks them up within a minute. The simulated series use the same names and labels as the real exporter, and each run's counters start from zero like a real run's. It generates every metric on this page. The errors match each run's status, and the phase durations add up to the run duration. Keep `--days` under the 15 days the local Prometheus retains. Pass `--seed` to get the same data every time.

To work on the fleet dashboard, generate a fleet of tenants instead of a single deployment:

```bash
make otel-backfill BACKFILL_ARGS=--fleet
```

This creates 8 tenants over 3 clusters (`prod-eu-west-1`, `prod-us-east-1`, `staging`), running every 15 minutes to every 2 hours. A few of them are set up to show the interesting states:

| Tenant | Behaviour |
|---|---|
| `prod-eu-west-1/data` | Flaky. |
| `prod-us-east-1/security` | Failing for the last 10 hours. |
| `staging/sandbox` | Stale: stopped 40 hours ago. |
| `prod-us-east-1/web` | Still on an older version (`v1.4.2`). |

Loading the same runs twice would count each of them twice. Before loading, the script asks Prometheus (`--prometheus`, `http://localhost:9091` by default) whether the same tenants already have runs in that time window, and refuses if they do. Pass `--force` to load anyway, or `--replace` to first delete the simulated series of the same tenants, for example after a change to the simulation. `--replace` only matches the versions the simulation uses (`v1.4.2`, `v1.5.0`, `v1.6.0-rc.1`), so real runs are kept. It needs the Prometheus admin API, which the local stack enables; recreate an older stack with `make otel-up`.

The simulated API requests carry a `status` code consistent with their class: mostly `404` and `403` for failed lookups, `409` for conflicts, `429` when GitLab rate limits the run (more during office hours and the incident), and `502`/`503` during the incident.
