---
title: Documentation
weight: 1
---

gitlab-sync is a CLI tool to synchronize GitLab projects and groups between two GitLab instances.
It is designed to be used in a CI/CD pipeline to automate the process of keeping two GitLab instances in sync.

## Features

- Synchronize projects / groups between two GitLab instances
- Recreates your git repository content in another location:
  - Enable Pull Mirroring for projects (requires GitLab Premium)
  - Clone the repository content from the source GitLab instance to the destination GitLab instance (on GitLab Free), optionally caching the clones on disk so the next run only fetches what changed
- Can add projects to CI/CD catalog (requires GitLab 19.3+ on the destination instance)
- Full copy of the project (description, icon, topics,...). Can also copy issues

## Where to go next

{{< cards >}}
  {{< card link="installation" title="Installation" subtitle="Docker image, go install, or build from source." >}}
  {{< card link="usage" title="Usage" subtitle="Command line arguments and environment variables." >}}
  {{< card link="mapping" title="Mirror mapping" subtitle="Choose what to synchronize and how." >}}
  {{< card link="freemium" title="Freemium destinations" subtitle="How mirroring works without GitLab Premium, and how to cache clones." >}}
  {{< card link="cli-reference" title="CLI reference" subtitle="The generated help output of every command." >}}
{{< /cards >}}
