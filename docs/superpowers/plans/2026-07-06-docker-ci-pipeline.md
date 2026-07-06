# Docker Build & Push CI Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions workflow that builds the three KubeWatch Docker images (agent, hub, dashboard) and pushes them to GHCR, and point the deploy manifests at the GHCR images.

**Architecture:** One workflow file with a matrix job over the three components. Each matrix leg builds `Dockerfile.<component>` for `linux/amd64` with `docker/build-push-action`, tags via `docker/metadata-action`, and pushes to `ghcr.io/gauravrautela/kubewatch-<component>` — except on pull requests, which build without pushing. Auth is the built-in `GITHUB_TOKEN`.

**Tech Stack:** GitHub Actions, docker/setup-buildx-action@v3, docker/login-action@v3, docker/metadata-action@v5, docker/build-push-action@v6.

**Spec:** `docs/superpowers/specs/2026-07-06-docker-ci-pipeline-design.md`

## Global Constraints

- Registry/image names: `ghcr.io/gauravrautela/kubewatch-agent`, `ghcr.io/gauravrautela/kubewatch-hub`, `ghcr.io/gauravrautela/kubewatch-dashboard` (all lowercase — GHCR requires it).
- Platform: `linux/amd64` only.
- Triggers: push to `main` (tags image `latest` + short SHA), git tags `v*` (tags image `X.Y.Z`, `X.Y`, `X`), pull requests to `main` (build only, no push).
- No repo secrets — auth uses `GITHUB_TOKEN` with `packages: write`.
- The three Dockerfiles already exist at repo root (`Dockerfile.agent`, `Dockerfile.hub`, `Dockerfile.dashboard`) and accept a `VERSION` build arg; do not modify them.
- Note (manual, post-merge): GHCR packages are private after first push; the user must flip each package to public in GitHub package settings. Not automatable in this plan.

---

### Task 1: GitHub Actions workflow

**Files:**
- Create: `.github/workflows/docker.yml`

**Interfaces:**
- Consumes: `Dockerfile.agent`, `Dockerfile.hub`, `Dockerfile.dashboard` at repo root (build context `.`), each accepting build arg `VERSION`.
- Produces: images `ghcr.io/gauravrautela/kubewatch-<component>` with tags `latest`+`sha-<short>` (main), semver (git tags), `pr-<N>` (PR metadata only, not pushed). Task 2 relies on the `latest` tag existing on these image names.

There is no unit-test cycle for CI YAML; the test is a syntax parse now (Step 2) and a live run after push (Task 3).

- [ ] **Step 1: Write the workflow file**

Create `.github/workflows/docker.yml` with exactly:

```yaml
name: docker

on:
  push:
    branches: [main]
    tags: ["v*"]
  pull_request:
    branches: [main]

permissions:
  contents: read
  packages: write

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        component: [agent, hub, dashboard]
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/gauravrautela/kubewatch-${{ matrix.component }}
          tags: |
            type=ref,event=pr
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
            type=sha,format=short
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          file: Dockerfile.${{ matrix.component }}
          platforms: linux/amd64
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          build-args: |
            VERSION=${{ steps.meta.outputs.version }}
          cache-from: type=gha,scope=${{ matrix.component }}
          cache-to: type=gha,mode=max,scope=${{ matrix.component }}
```

- [ ] **Step 2: Validate the YAML parses**

Run: `ruby -e "require 'yaml'; YAML.load_file('.github/workflows/docker.yml'); puts 'YAML OK'"`
Expected: `YAML OK`

(If `actionlint` is installed, also run `actionlint .github/workflows/docker.yml` — expected: no output, exit 0. Do not install it if missing.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/docker.yml
git commit -m "ci: build and push Docker images to GHCR"
```

---

### Task 2: Point deploy manifests at GHCR

**Files:**
- Modify: `deploy/agent.yaml:49`
- Modify: `deploy/hub.yaml:53`
- Modify: `deploy/dashboard.yaml:40`

**Interfaces:**
- Consumes: image names published by Task 1's workflow (`ghcr.io/gauravrautela/kubewatch-<component>:latest`).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Update the three image lines**

In `deploy/agent.yaml` line 49, replace:

```yaml
          image: gauravrautela/kubewatch-agent:v1            # e.g. ghcr.io/gauravrautela/kubewatch-agent:<TAG>
```

with:

```yaml
          image: ghcr.io/gauravrautela/kubewatch-agent:latest   # published by .github/workflows/docker.yml
```

In `deploy/hub.yaml` line 53, replace:

```yaml
          image: gauravrautela/kubewatch-hub:v1          # built from Dockerfile.hub
```

with:

```yaml
          image: ghcr.io/gauravrautela/kubewatch-hub:latest   # published by .github/workflows/docker.yml
```

In `deploy/dashboard.yaml` line 40, replace:

```yaml
          image: gauravrautela/kubewatch-dashboard:v1    # built from Dockerfile.dashboard (bundles the SPA)
```

with:

```yaml
          image: ghcr.io/gauravrautela/kubewatch-dashboard:latest   # published by .github/workflows/docker.yml (bundles the SPA)
```

- [ ] **Step 2: Verify only the intended lines changed and YAML still parses**

Run: `git diff deploy/ | grep '^[+-]' | grep -v '^[+-][+-]'`
Expected: exactly 6 lines — the 3 old `image:` lines removed, the 3 new `ghcr.io` lines added.

Run: `ruby -e "require 'yaml'; %w[deploy/agent.yaml deploy/hub.yaml deploy/dashboard.yaml].each { |f| YAML.load_stream(File.read(f)) }; puts 'YAML OK'"`
Expected: `YAML OK`

- [ ] **Step 3: Commit**

```bash
git add deploy/agent.yaml deploy/hub.yaml deploy/dashboard.yaml
git commit -m "chore: point deploy manifests at GHCR images"
```

---

### Task 3: Live verification via pull request

**Files:** none (verification only)

**Interfaces:**
- Consumes: the workflow from Task 1 (its `pull_request` trigger).
- Produces: evidence the pipeline builds all three images.

- [ ] **Step 1: Push the branch**

```bash
git push -u origin feat/kubewatch-capture-pipeline
```

- [ ] **Step 2: Open a PR to main (this makes the workflow run in build-only mode)**

```bash
gh pr create --base main --title "CI: build and push Docker images to GHCR" --body "Adds .github/workflows/docker.yml (matrix build of agent/hub/dashboard, push to GHCR on main and v* tags, build-only on PRs) and points deploy manifests at the GHCR images."
```

- [ ] **Step 3: Watch the workflow run to completion**

```bash
gh run watch $(gh run list --workflow=docker --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status
```

Expected: all three matrix legs (`agent`, `hub`, `dashboard`) succeed, exit code 0. On this PR run nothing is pushed to GHCR — pushing happens after merge to main.

If a leg fails, read the log with `gh run view --log-failed` and fix the Dockerfile/workflow issue before proceeding.

---

## Post-merge manual step (user)

After the first push to `main` publishes the images, make each package public: GitHub → your profile → Packages → `kubewatch-agent` / `kubewatch-hub` / `kubewatch-dashboard` → Package settings → Change visibility → Public.
