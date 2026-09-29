# AGENTS.md — plugin-status

Standalone plugin repo for the externalized `charly status` command
(`command:status`). The plugin is a Go module at `candy/plugin-status/` (module
path `github.com/opencharly/plugin-status/candy/plugin-status`); the root
`charly.yml` only declares `discover: candy` so the repo is a project and its
candy is scanned.

Canonical files:

- `candy/plugin-status/charly.yml` — the `plugin-status:` candy entity
  (`plugin:` block, `plan:` check).
- `candy/plugin-status/command.go` — the `charly status` Kong grammar.
- `candy/plugin-status/nested_tree.go` — declared-nested-tree pre-resolution.
- `candy/plugin-status/overlay.go` — the pure nested-deployment overlay fold
  (byte-parity golden in `overlay_golden_test.go`).
- `candy/plugin-status/render.go` — the output rendering.
- `candy/plugin-status/schema/status.cue` — the self-contained schema.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-internals:plugin` — the plugin authoring reference: the `plugin:`
  block, the unified Provider model, command-class dispatch, the per-plugin
  CUE-schema contract, placement. Load before touching the provider or schema.
- `/charly-core:charly-status` — the `charly status` surface this command owns.
- `/charly-internals:git-workflow` — before any git/PR action.

## Build / validate / test

- `go build ./...` in `candy/plugin-status/` — compile the plugin module.
- `go test ./...` in `candy/plugin-status/` — the plugin's Go tests
  (`overlay_golden_test.go`, `render_test.go`, `nested_tree_test.go`,
  `schema_serve_test.go`).
- `charly box validate` at the repo root — the structural check (the candy +
  `plugin:` block, CUE schema).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo has **no**
  per-repo candy gate.
- R10 witness: the full `charly status` end-to-end is exercised by the live R10
  bed (`check-local` / `check-pod`) plus the overlay golden.

## Modify this repo

- Edit the `plugin-status:` candy entity, the Go source, and
  `schema/status.cue` **together** — the schema is the single source for the
  generated types.
- The plugin is **compiled-in** and needs the in-proc reverse channel for its
  `verb:status-fanout` calls; it cannot run out-of-process.
- The live collection engine lives in `plugin-substrate`; the plugin reaches it
  by word over the reverse channel — do not add a core-side forward.

## Landing

- PR-only. Every change lands through a pull request; the org-required
  `charly/pr-validator` validates the diff and body and arms native auto-merge on
  PASS. Direct pushes to `main` are blocked.
- History lives in `CHANGELOG/` (written by `tag-on-merge` at merge time); the PR
  body IS the changelog.
- The authoritative rulebook is the umbrella `AGENTS.md` in
  `opencharly/opencharly` and `charly/AGENTS.md` in the charly repo. Do not
  restate its rules here.
