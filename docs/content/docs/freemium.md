---
title: Freemium destinations
weight: 30
---

Pull mirroring requires GitLab Premium. When the destination instance is not premium - or when `--destination-force-freemium` is passed to deliberately avoid pull mirrors on a premium/ultimate instance - gitlab-sync clones each source repository and pushes it to the destination itself. In that mode:

- The destination token needs to be able to push over HTTP; an `api` scoped token is enough.
- No mirror setting is written on the destination projects, and any pull mirror left over from a previous premium run is turned off first (GitLab keeps pull-mirrored repositories read-only, which would otherwise reject the push).
- Every ref is force-pushed, and the destination branches and tags the source no longer has are deleted, so the destination repository ends up an exact copy of the source one. The branch the destination `HEAD` points at is never deleted (a git server refuses that); when the source renames its default branch, the old one disappears on the next run, once the default branch has been moved through the API.
- The refs GitLab owns itself (`refs/merge-requests`, `refs/pipelines`, `refs/keep-around`, ...) are left untouched on the destination.
- Unless `mirror_trigger_builds` is enabled, the push asks the destination GitLab not to run CI for the branches and tags the synchronization updates, through the `ci.skip`, `ci.no_pipeline` and `integrations.skip_ci` push options (like `git push -o ci.skip -o ci.no_pipeline -o integrations.skip_ci`):
  - `ci.skip` works on every GitLab version, but GitLab still records one **skipped** pipeline per updated ref when the project has a CI configuration. No job runs.
  - `ci.no_pipeline` keeps that pipeline from being created at all, like a pull mirror with `mirror_trigger_builds: false`. It needs GitLab 18.11+ with the `ci_no_pipeline_push_option` feature flag enabled on the destination instance; older versions ignore it.
  - `integrations.skip_ci` keeps CI integrations such as Jenkins from being triggered.

  Pipeline execution policies can still enforce their own jobs, as GitLab ignores these options for them unless the policy allows it.
- Repositories are cloned into a temporary directory, so the container needs a writable `/tmp` (provided by the published image). Set `--cache-dir` to keep them instead.

## Caching the cloned repositories

Without a cache, every run downloads the full history of every repository, which gets
expensive on a schedule. `--cache-dir` points gitlab-sync at a directory where it keeps
the bare clones between runs:

```bash
gitlab-sync \
  --source-url https://gitlab.example.com \
  --source-token <source_gitlab_token> \
  --destination-url https://mycompany.example.com \
  --destination-token <destination_gitlab_token> \
  --mirror-mapping /path/to/mirror.json \
  --destination-force-freemium \
  --cache-dir /var/cache/gitlab-sync
```

- Each repository lands at a location derived from its source URL, so a run always
  reuses the clone it produced last time:

  ```text
  /var/cache/gitlab-sync/
    gitlab.example.com/
      group/subgroup/repo.git/
      group/other.git/
  ```

- An existing clone is refreshed rather than downloaded again, with a forced and pruning
  fetch: new branches and tags are downloaded, the ones deleted at the source are deleted
  locally, and a rewritten history replaces the one held on disk. Caching therefore never
  changes what ends up on the destination, only how much is transferred to get there.
- A cache entry that cannot be opened or refreshed is thrown away and cloned again, so a
  run interrupted halfway never poisons the following ones.
- Entries are locked while in use, both between the goroutines of one run and between
  concurrent runs (parallel CI jobs sharing a volume, an overlapping cron). A lock left
  behind by a run that died is detected and broken automatically.
- Entries that go unsynced for longer than `--cache-max-age` are removed at the start of a
  run. Pass `--cache-max-age 0` to keep them forever and manage the directory yourself.
- The option is ignored on a premium destination, which uses pull mirroring and never
  clones anything locally.

With the published image, mount a volume on the cache directory so it survives the container:

```bash
docker run --rm \
  -e SOURCE_GITLAB_URL=<source_gitlab_url> \
  -e SOURCE_GITLAB_TOKEN=<source_gitlab_token> \
  -e DESTINATION_GITLAB_URL=<destination_gitlab_url> \
  -e DESTINATION_GITLAB_TOKEN=<destination_gitlab_token> \
  -e MIRROR_MAPPING=/home/gitlab-sync/mirror.json \
  -e GITLAB_SYNC_CACHE_DIR=/cache \
  -v <my_mapping_json_file>:/home/gitlab-sync/mirror.json:ro,Z \
  -v gitlab-sync-cache:/cache \
  ghcr.io/boxboxjason/gitlab-sync:latest --destination-force-freemium
```
