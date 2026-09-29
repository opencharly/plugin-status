# plugin-status

Runtime status for OpenCharly — the externalized `charly status` command.

The plugin owns the command end to end: the Kong grammar, the declared-nested-tree
pre-resolution, the pure nested-deployment overlay fold, and the render output
(table / detail / JSON, `--all`, `--nested`). It is a **compiled-in** command
plugin because its `Invoke(OpRun)` needs the in-proc reverse channel to reach
`verb:status-fanout` — the live collection engine in the sibling
`plugin-substrate` that fans out across every deployment substrate
(pod/vm/kubernetes/local/android) and probes live tools. The out-of-process
`CliMain` path has no reverse channel.

The plugin also serves `sdk.OpStatusCollect` — the programmatic status-collection
API (distinct from the lifecycle `OpStatus` a substrate plugin serves).

## What it provides

| Capability | Surface |
|---|---|
| `command:status` | the `charly status` CLI — table / detail / JSON, `--all`, `--nested` |
| `OpStatusCollect` | the programmatic status-collection API over `InvokeProvider(class:command, word:status, op:status-collect)` |

## How to use it

```bash
charly status
charly status --all
charly status --nested
charly status <name> --json
```

## Layout

- `candy/plugin-status/` — the plugin module: `command.go` (the Kong grammar),
  `nested_tree.go` (declared-tree pre-resolution), `overlay.go` (the pure overlay
  fold), `render.go` (output), `provider.go` / `plugin.go`,
  `schema/status.cue`, `cmd/serve/main.go`.
- `charly.yml` — the root project manifest (`discover: candy`).
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.

## Related

- Owning skill: `/charly-core:charly-status` — the `charly status` surface. This
  candy carries no `skill:` entity of its own; the gap is tracked in
  [opencharly/opencharly#291](https://github.com/opencharly/opencharly/issues/291).
- `/charly-internals:plugin` — the plugin/provider model.
- [`opencharly/charly`](https://github.com/opencharly/charly) — the charly CLI.
