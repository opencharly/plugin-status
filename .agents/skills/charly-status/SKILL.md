---
name: charly-status
description: |-
  Service status display with tool probes and device detection — `charly status`, checking container state, tool availability, port mapping, or JSON status output. Named `charly-status` (not `status`).
---

# charly status -- Service Status Display

## Overview

`charly status` is the **unified deployment-status surface**: one table (or one
JSON array, or a single-deployment detail view) showing every charly deployment
across all five substrates — **pod, vm, kubernetes, local, android** — side by side.
A leading **KIND** column / `"kind"` JSON field discriminates which substrate
each row came from.

The architecture is a **generic word-dispatched fan-out**, not a registry: ALL
FIVE substrates (pod/vm/kubernetes/local/android) are collected by ONE compiled-in
provider (`candy/plugin-substrate`), served on its kind-provider `Invoke` as
`sdk.OpStatusCollect`, dispatched by word — there is no `SubstrateCollector`
interface or `init()`-time registry left in core (both were deleted once the
last substrate, android, moved out). `flatCollector.collectFlat`
(candy/plugin-substrate/status_flat.go — the former core `Collector.collectFlat`
is DELETED, K-wave 2) calls each word over the registry + reaches the
reverse-channel executor so the plugin can
call back (`InvokeProvider("build","project", OpResolve)` for the resolved-project envelope for vm/kubernetes — the former `HostBuild("resolved-project")` seam is DELETED, `InvokeProvider`
for vm→libvirt), merges the rows, applies the DEPLOY-CONE enrichment core alone
can still do (pod tunnel/volume/port fallback, vm SSH-port/network), and sorts
by `(Kind, image)`. The pod collector still does the batched `podman ps` +
`podman inspect` + worker-pool probe fan-out (host probes — CDP/VNC — in
parallel goroutines; guest probes — supervisord/dbus/charly/wl/sway — batched into
one `podman exec sh -c`); the other collectors read their own backends
(libvirt for vm, client-go for live kubernetes, the install ledger for local, adb for
android) — ALL inside the plugin now.

**Graceful degradation is the contract.** A word resolve miss or an
`Invoke`/collect error logs a single `WARNING:` to stderr and contributes zero
rows, but NEVER aborts the whole command. So `charly status` on a host with
only podman shows the pod rows and silently omits vm/kubernetes/android; the surface
always renders what it can.

Source layout:

The `charly status` surface is split across TWO homes now (K6 retired the
third): the **command:status plugin** owns the CLI + render + the PURE nested
overlay + the declared-nested-tree pre-resolution; the **substrate plugin**
(candy/plugin-substrate) owns EVERYTHING ELSE — ALL FIVE per-substrate
COLLECTORS (pod/vm/kubernetes/local/android), the FLAT fan-out + deploy-cone
ENRICHMENT (formerly charly/status_collector.go's Collector — the "stays core,
registry-boundary blocker" verdict was reopened and reversed: the registry
fan-out dissolves into a direct in-package call once the orchestration moves
into the plugin that already owns the per-word collectors, and every other
dependency was already sdk-portable via K4 #64), the probes, and the
externalized `charly reap-orphans` command. **Core keeps NO status business
logic at all** — the plugin InvokeProvider(verb:status-fanout) directly (the
former `status-substrate` HostBuild seam is DELETED, K-wave 2).

- `candy/plugin-status/command.go` — the `charly status` Kong grammar + dispatch
  (the `--nested` and `--json` flags live here); InvokeProvider(verb:status-fanout)
  for the flat rows, calls the plugin's own `buildStatusRootsTree`
  for the declared tree, applies the PURE nested overlay + renders.
- `candy/plugin-status/render.go` — the unified `DeploymentStatus` rendered
  shape + `RenderTable` / `RenderDetail` / `RenderJSON` / `RenderJSONOne` +
  cell formatters.
- `candy/plugin-status/overlay.go` — the PURE nested-overlay fold.
- `candy/plugin-status/nested_tree.go` — the declared-nested-tree pre-resolution
  (K5, relocated from charly/status_nested.go's buildStatusRootsTree): resolves
  the merged project + per-machine deploy tree directly (`HostBuild("resolved-
  project")` + `deploykit.LoadDeployConfig`) into the wire-safe
  `[]spec.StatusNestedNode` shape the overlay folds, including the `--nested`
  live-probe leg (`ResolveDeployChain` + `NestedExecutor`, the SAME primitive
  `charly deploy add` / `charly check live parent.child` use).
- `candy/plugin-status/command.go` (`hostStatusSubstrate`) — the generic
  `status-substrate` F10 forward is DELETED with `charly/status_substrate_host.go`
  (K-wave 2): `command:status` now `InvokeProvider(verb:status-fanout)` DIRECTLY over
  the in-proc reverse channel, threads the executor, and returns the reply verbatim.
  No status-specific logic remains in core.
- `candy/plugin-substrate/status_flat.go` — `flatCollector.collectFlat` (the
  substrate fan-out + merge + sort) / `flatCollector.collectSingle`;
  `collectWord` (a DIRECT in-package call to `statusCollect` for ALL FIVE
  words — no registry, no wire round-trip); `enrichOne` (the DEPLOY-ENRICHMENT
  half — charly.yml tunnel + image-label fallback, applied to the pod
  collector's LIVE rows); `enrichVmRow` (SSH-port/network from the matching
  target:vm deploy entry); `lookupDeploy`; `resolveSystemdState`;
  `listProvisionedSecretNames`; `parsePortStrings`; `formatTunnelSummary`.
  Served by `verb:status-fanout`'s `Invoke(sdk.OpStatusCollectAll)`
  (plugin.go) — an INTERNAL-ONLY verb, never a CLI subcommand, mirroring the
  `verb:libvirt`/`verb:credential`/`verb:arbiter` internal-dispatch precedent.
- `candy/plugin-substrate/status_collect.go` — the COLLECTOR OpStatusCollect
  dispatch (by word pod/vm/kubernetes/local/android — ALL FIVE now).
- `candy/plugin-substrate/status_pod.go` — the pod LIVE collection (SnapshotAll +
  the worker-pool fan-out + `collectPodLive` row builder + `runPodProbes` +
  `applyQuadletDescription` + `enabledQuadlets` + `parseQuadletDescription` +
  `formatLiveMounts`).
- `candy/plugin-substrate/status_vm.go` — the vm collector (libvirt domains →
  vm rows, `Source="libvirt"`).
- `candy/plugin-substrate/status_kubernetes.go` — the kubernetes collector (cluster
  workloads; live client-go probing under `--nested`).
- `candy/plugin-substrate/status_android_collect.go` — the android collector
  (declared `android` devices → rows via adb `host:devices`, `Source="adb"`).
- `candy/plugin-substrate/status_local.go` — the local install-ledger collector
  (`Source="ledger"`).
- `candy/plugin-substrate/status_probes.go` — `Probe` / `HostProbe` /
  `GuestProbe` interfaces and the concrete probes (`SupervisordProbe`,
  `DbusProbe`, `CharlyProbe`, `WlProbe`, `SwayProbe` are guest; `CdpProbe`,
  `VncProbe` are host). `runGuestProbes` builds a single concatenated shell
  script with per-probe markers and splits the stdout chunks back out.
- `candy/plugin-substrate/command_reap_orphans.go` — `ReapOrphansCmd` (the
  externalized `charly reap-orphans` command, K5, relocated from
  charly/status_reap.go), a new `command:reap-orphans` capability on the SAME
  provider; its vm-liveness probe reaches the verb:libvirt peer provider via
  `Executor.InvokeProvider` (F10) instead of a core-private accessor.
- `sdk/enginekit` — `EngineClient` (the only place that touches podman/docker),
  `ContainerSnapshot`, structured `PortMapping` — the sdk kit the substrate
  plugin's pod-live collection imports.
- `spec/spec/status_types.go` — `SubstrateKind` + `StatusFromState` (the shared
  state-vocab mapper, single-sourced for every plugin collector + the host's
  enrichment).

## Quick Reference

| Action | Command | Description |
|--------|---------|-------------|
| Table (running) | `charly status` | Show all deployments across every substrate (pod/vm/kubernetes/local/android) |
| Table (all) | `charly status --all` | Include stopped and enabled services |
| Nested probe | `charly status --nested` | Probe nested children + live kubernetes workloads (multi-hop, slower) |
| Detail | `charly status <image>` | Key-value detail for one service |
| Detail (instance) | `charly status <image> -i <inst>` | Key-value detail for one instance |
| JSON output | `charly status --json` | Machine-readable JSON (KIND-discriminated, structured ports, nested tree) |
| Reap orphans | `charly reap-orphans` | Clean up ephemerals whose underlying resource is gone |

## Table Mode Columns

Columns: `KIND  IMAGE  STATUS  PORTS  TUNNEL  DEVICES  TOOLS`. Rows are sorted
by `(KIND, IMAGE)` so all rows of one substrate group together. Nested children
render as indented IMAGE-cell rows (`  └─ <child>`) under their parent.

| Column | Description |
|--------|-------------|
| KIND | Substrate discriminator: `pod` / `vm` / `kubernetes` / `local` / `android` (`-` when unset). Names which collector produced the row |
| IMAGE | `image` for base deploys, `image/instance` for multi-instance (matches `deployKey` shape); for a vm/local/android row it is the vm name / local-template label / declared android device key |
| STATUS | `running` / `stopped` / `enabled` / `failed` / `dead` / `paused`; substrate-specific values: `applied` (local ledger), `online` / `offline` / `absent` (android), `declared` / `reachable` / `unreachable` (nested children) |
| PORTS | Sorted, deduped host port numbers from runtime `podman ps` (charly.yml / image labels are fallbacks for non-running rows) |
| TUNNEL | `provider (all ports)` / `provider (ports H,H,H)` / `-` — read from charly.yml |
| DEVICES | Compact tokens (`gpu`, `dri`, `kvm`, `fuse`, `tun`) sorted alphabetically |
| TOOLS | Live-probed tools — port-based show `name:port`, socket-based show just `name` |

## Detail Mode Fields

`charly status <image>` shows:

| Field | Example |
|-------|---------|
| Kind | `pod` (omitted when unset) |
| Image | `jupyter` (box short name) |
| Image ref | `ghcr.io/opencharly/jupyter:2026.162.1319` — the RUNNING container's image ref:tag (runtime truth; JSON `image_ref`; the R9 freshness check reads this) |
| Status | `running` |
| Container | `charly-jupyter` |
| Secrets | `charly-jupyter-db-password` — engine secrets provisioned for this box (detail view only; JSON `secrets`) |
| Mode | `quadlet` |
| Ports | `8888:8888/tcp` — HOST:CONTAINER/proto, comma-joined when there are several (`longPorts`). The detail view carries BOTH sides; the table's PORTS column carries host ports only |
| Devices | `nvidia (CUDA)` |
| Tools | `cdp:9222, vnc:5900, sway, wl` |
| Volumes | `data: bind /home/user/data` |
| Network | `host` |
| Tunnel | `cloudflare: jupyter.example.com` |
| Nested | `android device (online)` — one line per declared nested child |

The single-image detail path is pod-scoped (`Collector.Single` covers the
podman/docker substrate). For the cross-substrate view use the table.

## Nested deployments

A deploy can declare a nested tree (`pod → android`, `vm → pod`, `vm → host`,
…). `charly status` reflects it WITHOUT a dedicated "nested" collector — a nested
child's venue is always REACHED THROUGH its parent, so `applyNestedOverlay`
post-processes the already-merged flat rows: it reads the DECLARED tree from
the merged deploy config (project `charly.yml` incl. folded disposable check
deploys + `~/.config/charly/charly.yml`) and attaches each declared child to its
parent row's `Nested[]`.

**Dedup — a declared nested child appears exactly once.** A child substrate
collector may ALSO surface a flat top-level row for the same deployment: an
`AndroidCollector` row keyed on the dotted path (`<parent>.device`), or a
nested-pod row keyed on the flattened container name (`NestedContainerName` →
`<seg1>_<seg2>`). When the overlay finds such a flat row, it **MOVES** that
row's real collected data (status / uptime / container / ports / devices /
tools / volumes / network / tunnel) into the nested position — preserving its
real `Source` (`adb`, `podman`, …), NOT restamping `nested` — and **REMOVES**
the flat row from the top level. So a nested android device shows ONLY under
its parent pod's `nested[]`, never also as a flat row. A child with NO flat
match keeps the synthesized declared row (`Source="nested"`).

- **Default** (`charly status`): a child with a flat match inherits that flat row's
  live `status`/`uptime`/… (and real `Source`); a child with no flat match
  reads `declared` (`Source="nested"`). No multi-hop work, no extra
  subprocesses.
- **`charly status --nested`**: each child's LIVE venue is probed through the real
  multi-hop chain (`ResolveDeployChain` → `NestedExecutor`, the SAME primitive
  `charly deploy add` and `charly check live parent.child` use — no bespoke nested dial)
  under a STRICT 4-second per-child context deadline. A timed-out / failing
  child renders `unreachable`; the table is NEVER blocked. The deadline is a
  context cancellation, never a sleep/retry loop. `--nested` also turns on live
  kubernetes-workload probing and the android `sys.boot_completed` readiness poll.

A synthesized (no-flat-match) child carries `Source="nested"` in JSON so a
consumer tells a declared-only child apart from a natively-collected substrate
row; a MOVED child carries its origin collector's real `Source`.

## Tool Probes

Two probe kinds. Host probes (cdp, vnc) run from the operator host using
the snapshot's `HostPortFor(ctrPort, proto)` lookup — no extra `podman
port` / `podman inspect` calls. Guest probes (supervisord, dbus, charly, wl,
sway) batch into ONE `podman exec sh -c` per container; each probe's
snippet emits a `KEY=value` line that its `Parse` recognises. The batcher
delimits sections with `===PROBE:<name>===` / `===PROBE_END:<name>===`
markers.

| Tool | Kind | Snippet / Probe | Display |
|------|------|-----------------|---------|
| supervisord | guest | `command -v supervisorctl && supervisorctl status` | `supervisord` (with `N/M running` detail in detail view) |
| dbus | guest | `pgrep -x dbus-daemon` + scan for `swaync`/`mako`/`dunst` | `dbus` (notifier list in detail view) |
| charly | guest | `command -v charly && charly version` | `charly` (CalVer detail) |
| wl | guest | `command -v wtype/wlrctl/grim/pixelflux-screenshot` | `wl` (detail lists available tools) |
| sway | guest | discover SWAYSOCK then `swaymsg -t get_outputs` | `sway` (output dimensions in detail) |
| cdp | host | HTTP GET `:HOST_PORT/json` (port from snapshot) | `cdp:HOST_PORT` |
| vnc | host | TCP dial + RFB banner read | `vnc:HOST_PORT` |

Adding a new probe: implement `HostProbe` (network) or `GuestProbe`
(in-container) in `candy/plugin-substrate/status_probes.go` and register in the
package-level `hostProbes` / `guestProbes` slice. No other file needs editing.

## JSON output schema

`charly status --json` emits an array of `DeploymentStatus` objects — one per row,
across every substrate. The leading `"kind"` field is the substrate
discriminator; `"source"` records provenance (`podman` / `libvirt` / `ledger` /
`adb` / `nested`); `"ports"` is a structured array (not `[]string`); `"nested"`
is the recursive child tree (omitted when empty):

```json
{
  "kind": "pod",
  "image": "selkies-desktop",
  "image_ref": "ghcr.io/opencharly/selkies-desktop:2026.162.1319",
  "instance": "work",
  "status": "running",
  "container": "charly-selkies-desktop-work",
  "ports": [
    { "host_ip": "127.0.0.1", "host_port": 9240, "container_port": 9222, "protocol": "tcp" }
  ],
  "tunnel": "tailscale (all ports)",
  "tools": [ { "name": "cdp", "status": "ok", "port": 9240, "detail": "3 tabs" } ],
  "run_mode": "quadlet",
  "source": "podman"
}
```

A deployment with a declared nested tree (e.g. `pod → android`) carries its
children under `"nested"`. A child that surfaced as a flat substrate row is
MOVED here with its real `"source"` (`adb`) and its collected data; a child
with no flat row is synthesized (`"source": "nested"`, `"status": "declared"`):

```json
{
  "kind": "pod",
  "image": "android-emulator",
  "status": "running",
  "container": "charly-android-emulator",
  "run_mode": "quadlet",
  "source": "podman",
  "nested": [
    { "kind": "android", "image": "device", "status": "online", "container": "emulator-5554", "source": "adb" },
    { "kind": "android", "image": "device-net", "status": "declared", "source": "nested" }
  ]
}
```

Because the JSON encoder indents (`SetIndent("", "  ")`), the on-the-wire
substring for a substrate row is `"kind": "pod"` — a SPACE after the colon.
Check `command` checks that grep `charly status --json` output assert on the spaced
form (e.g. `contains: '"kind": "vm"'`). The four disposable check deploys each carry a
`status-shows-*` member check that proves the live `charly status --json`
reports the right kind (and, for android, the `"nested"` tree).

Single-image (`charly status <image> -i <inst> --json`) emits one object,
not an array.

## Source-of-truth priority for the PORTS column

1. Runtime `podman ps` mappings (`ContainerSnapshot.Ports`) — wins for
   running containers.
2. `charly.yml` `port:` (parsed via canonical `ParsePortMapping` —
   handles the `127.0.0.1:H:C/proto` IPv4-prefixed form correctly) —
   used when runtime data is empty.
3. Image-label fallback (`ResolveNewestLocalCalVer` + `ExtractMetadata`)
   — last resort for stopped/enabled rows. **Lookup uses the BASE image
   name from the parsed quadlet description** (e.g. `selkies-desktop`),
   not the joined container name (`selkies-desktop-185.52.136.164`).

## `Volumes:` field — live mounts vs label fallback

The `Volumes:` field is rendered from THREE sources, in priority order:

1. **Live mounts** (`podman inspect .Mounts[]`) — wins for running containers. Format: `<name>: <source> -> <dest>` for named volumes, `bind: <source> -> <dest>` for bind mounts. Encrypted FUSE binds (source matches `<...>/encrypted/<vol>/plain`) get an `(enc)` suffix so the display distinguishes a `type: encrypted` deploy override from a plain bind.
2. **charly.yml** volume names — fallback for stopped/enabled containers when no live mounts are available. Lists just the volume names from the deploy entry.
3. **Image OCI label** (`ExtractMetadata`) — last-resort fallback when neither runtime nor deploy data is present. Format: `<volume-name> -> <container-path>` (the layer-declared default).

This means a volume deployed with `--bind <name>=<path>` or `--encrypt <name>` shows up in the live form for running containers — what the container is ACTUALLY mounting, including the gocryptfs FUSE plain dir for encrypted volumes. Showing live mounts (rather than the image-label default) is what lets the operator tell, from `charly status` alone, whether an encrypted volume's gocryptfs FUSE is actually mounted: a running container binding `<...>/charly-immich-cache/plain -> /home/user/.immich/cache` with the FUSE unmounted would otherwise be writing plaintext over the cipher tree, and the live form makes that visible.

For programmatic queries the same data is in `charly status --json`'s `volumes` array. Source: `candy/plugin-substrate/status_pod.go:formatLiveMounts` (relocated from core, P14a) + `sdk/enginekit` (the `MountInfo` parser). Tested by `candy/plugin-substrate/status_test.go` (the relocated live-mounts cases: the encryption-path detector + the renderer).

Authoritative direct queries (when you need the raw mount data):

```bash
charly status <image> --json    # volumes[] carries the live mounts
charly deploy show <image>
```

## Usage

```bash
# Quick overview of all deployments across every substrate
charly status

# Include stopped services
charly status --all

# Probe nested children + live kubernetes workloads (multi-hop, slower)
charly status --nested

# Detailed info for one service
charly status jupyter

# JSON for scripting
charly status --json | jq '.[] | select(.status == "running")'

# Filter to one substrate
charly status --json | jq '.[] | select(.kind == "vm")'

# List declared nested children
charly status --json | jq '.[] | select(.nested) | .nested[].image'
```

## Cross-References

- `/charly-build:pull` -- Prerequisite: fetch the image into local storage; handles remote refs (`@github.com/...`) and the `ErrImageNotLocal` recovery path

- `/charly-core:start` -- start a service
- `/charly-core:stop` -- stop a service (via `/charly-core:service`)
- `/charly-core:logs` -- view service logs (via `/charly-core:service`)
- `/charly-core:service` -- full service lifecycle management

Live-deploy verification: see /charly-check:check (the 11 Testing Standards) and /charly-internals:disposable.
