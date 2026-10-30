---
title: Mirror mapping
weight: 40
---

The JSON mapping file is used to define the projects and groups to be synchronized between the two GitLab instances. You also define the copy options for each project / group.

## Options

Allowed options are:

| Option | Description |
| -------- | ------------- |
| `destination_path` | The path to the project / group on the destination GitLab instance. |
| `ci_cd_catalog` | Whether to add the project to the CI/CD catalog. ⚠️ Requires GitLab 19.3+ on the destination instance, since it relies on the `cicd_catalog_enabled` project API field introduced in that version. |
| `mirror_issues` | Whether to copy issues from the source project to the destination project. |
| `visibility` | The visibility level of the project on the destination GitLab instance. Can be `public`, `internal`, or `private`. |
| `mirror_trigger_builds` | Whether to trigger builds on the destination project when a push is made to the source project (default: `false`). On a freemium destination, `false` makes gitlab-sync push with push options that keep the synchronization from running CI (see [Freemium destinations]({{< relref "freemium" >}})). ⚠️ Earlier versions of gitlab-sync always started pipelines on freemium destinations: set it to `true` to keep that behaviour. |
| `mirror_releases` | Whether to mirror releases from the source project to the destination project. |
| `mirror_release_assets` | Whether to also keep the release asset links (and their files) in sync (requires `mirror_releases`), including on releases that already exist on the destination. ⚠️ Deletes destination releases, links and files that no longer exist on the source. See [Release assets](#release-assets). |
| `claim_ownership` | Whether to make the authenticated user (the owner of the destination token) an Owner of the destination project / group. On a group, the user is added as an Owner. On a project, the access level is reasserted on every run, so a user that was added at a lower level, or by a previous run under a different account, is promoted to Owner. |

Be aware that the destination path must be unique for each project / group. If you try to synchronize a project / group with the same destination path as an existing project / group, the synchronization will fail.

Also, the destination namespace must exist on the destination GitLab instance. If the namespace does not exist, the synchronization will fail.

## Example

```json
{
  "projects": {
    "existingGroup1/project1" : {
      "destination_path": "existingGroup64/project1",
      "ci_cd_catalog": true,
      "mirror_issues": false,
      "visibility": "public",
      "mirror_trigger_builds": false,
      "mirror_releases": false,
      "mirror_release_assets": false
    }
  },
  "groups": {
    "existingGroup152" : {
      "destination_path": "existingGroup64/existingGroup152",
      "ci_cd_catalog": true,
      "mirror_issues": false,
      "visibility": "public",
      "mirror_trigger_builds": false,
      "mirror_releases": false,
      "mirror_release_assets": false
    }
  }
}
```

## Release assets

When `mirror_release_assets` is enabled, the destination release links are kept in sync with the source release links (links are matched by name), keeping their `name`, `link_type` and `direct_asset_path`:

- **Generic package on the source instance** (`/api/v4/projects/:id/packages/generic/:name/:version/:file`): the file is republished to the destination project generic package registry under the same package name, version and file name, and the link points to that copy. On every run, the checksum GitLab stored for the source file is compared with the one of the destination copy (no download is needed for that): the file is only copied again when the destination copy is missing or differs. Previous copies are kept in the package, GitLab serves the newest one. When the file no longer exists on the source (the source package no longer lists it and its download answers 404), its destination copies are deleted.
- **Any other URL on the source instance** (job artifacts, other package registries, ...): the file is downloaded and stored in the destination project generic package registry as `release-assets/<tag>/<file>`, and the link points to that copy. These sources have no checksum to compare, so the file is copied once: when the destination release does not have the link yet. A source file that disappears while its link stays (e.g. an expired job artifact) is not detected.
- **External URLs**: the link is copied unchanged, when the destination release does not have it yet.

{{< callout type="warning" >}}
Links of a destination release that its source release no longer has are **deleted**, including links added by hand on the destination. When such a link points to a generic package file of the destination project, that file is deleted too, unless a link of another source release is still copied to it. Destination releases whose tag no longer has a release on the source are **deleted** too (the git tag itself is left to the repository mirroring), along with the files their links point to in the destination project generic package registry, with the same exception. With `mirror_releases` alone (without `mirror_release_assets`), nothing is ever deleted. Use `--dry-run` to see what would be deleted.
{{< /callout >}}

Files are downloaded with the source token. Web (non-API) URLs of private projects may not accept it: when the source instance answers with its sign in page, the asset is reported as failed rather than mirrored.
