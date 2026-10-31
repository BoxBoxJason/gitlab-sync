---
title: gitlab-sync
description: "gitlab-sync is a Go CLI that copies and mirrors GitLab projects and groups between two instances, on GitLab Premium or Free, from a CI/CD pipeline."
layout: hextra-home
---

{{< hextra/hero-headline >}}
  Keep two GitLab instances in sync
{{< /hextra/hero-headline >}}

{{< hextra/hero-subtitle >}}
  A single-binary CLI that copies and mirrors GitLab projects and groups between instances, built to run from a CI/CD pipeline.
{{< /hextra/hero-subtitle >}}

{{< spacer >}}

{{< hextra/hero-button text="Get started" link="docs/installation" >}}

{{< spacer "3rem" >}}

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Premium or free"
    subtitle="Uses pull mirroring on GitLab Premium, and clones and pushes the repositories itself on GitLab Free, optionally caching the clones between runs."
  >}}
  {{< hextra/feature-card
    title="More than the code"
    subtitle="Copies descriptions, icons, topics, issues, releases and release assets, and can publish projects to the CI/CD catalog."
  >}}
  {{< hextra/feature-card
    title="Declarative"
    subtitle="Describe what to synchronize in a JSON mapping file, with options per project or group, and preview the result with a dry run."
  >}}
{{< /hextra/feature-grid >}}
