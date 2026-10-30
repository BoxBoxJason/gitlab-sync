---
title: Usage
weight: 20
---

The CLI requires no dependencies and can be run directly. It is available as a single binary executable.

The mirroring configuration can be passed by either command line arguments or environment variables.
If mandatory arguments are not provided, the program will prompt for them, unless `--no-prompt` is set.

{{< callout type="info" >}}
The full `--help` output of the CLI is available in the [CLI reference]({{< relref "cli-reference" >}}).
{{< /callout >}}

## Arguments

| Argument | Environment Variable equivalent | Mandatory | Description |
| ---------- | ------------------------------- | ----------- | ------------- |
| `--help` or `-h` | N/A | No | Show help message and exit |
| `--version` | N/A | No | Show version information and exit |
| `--verbose` or `-v` | N/A | No | Enable verbose output |
| `--no-prompt` or `-n` | `NO_PROMPT` | No | Never prompt for missing values, fail instead (any non-empty value of the environment variable enables it) |
| `--dry-run` | N/A | No | Perform a dry run without making any changes |
| `--source-url` | `SOURCE_GITLAB_URL` | Yes | URL of the source GitLab instance |
| `--source-token` | `SOURCE_GITLAB_TOKEN` | No | Access token for the source GitLab instance |
| `--source-big` | `SOURCE_GITLAB_BIG` | No | Specify if the source GitLab instance is a big instance (default: false, any non-empty value of the environment variable enables it) |
| `--destination-url` | `DESTINATION_GITLAB_URL` | Yes | URL of the destination GitLab instance |
| `--destination-force-freemium` or `-f` | N/A | No | Force the destination GitLab to be treated as a non-premium instance (default: false) |
| `--destination-force-premium` or `-p` | N/A | No | Force the destination GitLab to be treated as a premium instance (default: false) |
| `--destination-token` | `DESTINATION_GITLAB_TOKEN` | Yes | Access token for the destination GitLab instance |
| `--destination-big` | `DESTINATION_GITLAB_BIG` | No | Specify if the destination GitLab instance is a big instance (default: false, any non-empty value of the environment variable enables it) |
| `--mirror-mapping` | `MIRROR_MAPPING` | Yes | Path to a JSON file containing the [mirror mapping]({{< relref "mapping" >}}) |
| `--retry` or `-r` | N/A | No | Number of retries for failed GitLab API requests (default: 3, `-1` for no limit) |
| `--log-file` | `GITLAB_SYNC_LOG_FILE` | No | Path to a log file for output logs (default: `none`, only outputs logs to stderr) |
| `--cache-dir` | `GITLAB_SYNC_CACHE_DIR` | No | Directory keeping the cloned repositories between runs, to avoid cloning them again (freemium mirroring only, default: `none`, caching disabled). See [caching]({{< relref "freemium#caching-the-cloned-repositories" >}}) |
| `--cache-max-age` | `GITLAB_SYNC_CACHE_MAX_AGE` | No | Delete the cached repositories that have not been synchronized for that long (Go duration, `0` keeps them forever, default: `168h`, 7 days) |
| `--user-agent` | `GITLAB_SYNC_USER_AGENT` | No | User-Agent sent with every GitLab API request; git clone, fetch and push requests keep the go-git default (default: `gitlab-sync/<version>`) |

## Example

```bash
gitlab-sync \
  --source-url https://gitlab.example.com \
  --source-token <source_gitlab_token> \
  --destination-url https://mycompany.example.com \
  --destination-token <destination_gitlab_token> \
  --mirror-mapping /path/to/mirror.json
```

Add `--dry-run` to see what would change without writing anything to the destination.
