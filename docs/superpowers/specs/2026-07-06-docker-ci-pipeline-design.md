# Docker Build & Push CI Pipeline — Design

**Date:** 2026-07-06
**Status:** Approved

## Goal

Automatically build the three KubeWatch Docker images (agent, hub, dashboard) and
publish them to a public registry via GitHub Actions.

## Decisions

- **Registry:** GHCR — `ghcr.io/gauravrautela/kubewatch-<component>`
- **Triggers:**
  - Push to `main` → build & push, tagged `latest` + short commit SHA
  - Git tag `v*` (e.g. `v1.2.0`) → build & push, tagged `v1.2.0`, `1.2`, `1`
  - Pull requests → build only (no push), serving as a Dockerfile CI check
- **Platform:** `linux/amd64` only
- **Auth:** built-in `GITHUB_TOKEN` with `packages: write` — no repo secrets needed

## Architecture

One workflow file, `.github/workflows/docker.yml`, containing a single job with a
matrix over `component ∈ {agent, hub, dashboard}`. Each matrix leg:

1. Checks out the repo
2. Sets up Buildx
3. Logs in to GHCR (skipped on pull requests)
4. Generates tags via `docker/metadata-action`
5. Builds `Dockerfile.<component>` with `docker/build-push-action`,
   pushing unless the event is a pull request, with GitHub Actions layer
   caching (`cache-from`/`cache-to: type=gha`) scoped per component

## Manifest updates

`deploy/agent.yaml`, `deploy/hub.yaml`, and `deploy/dashboard.yaml` currently
reference Docker Hub–style images (`gauravrautela/kubewatch-*:v1`). Update them to
`ghcr.io/gauravrautela/kubewatch-<component>:latest` so clusters pull what CI
publishes.

## One-time manual step (documented, not automated)

GHCR packages are private on first push. After the first successful workflow run,
flip each of the three packages to **public** in GitHub → Packages → package
settings → Change visibility.

## Out of scope

- arm64 / multi-arch builds
- Docker Hub publishing
- Deploying to a cluster from CI
