package status

import (
	"bytes"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// overlay_golden_test.go — the candy's byte-parity anchor for applyNestedOverlay (the PURE
// fold ported from charly/status_nested.go, P14a chunk 2b). It constructs a FIXED
// []spec.DeploymentStatus (flat rows) + []spec.StatusNestedNode (the host-pre-resolved declared
// tree) exercising every preserved semantic in one scenario — claim/inherit/dedup, synthesize
// declared, sorted order, --nested live-status override, and an unrelated flat row left alone —
// runs applyNestedOverlay + RenderJSON, and asserts the EXACT JSON bytes. The golden was locked
// from this file's own first green run (never hand-computed) — see the R10 self-verify output.
//
// Scenario (mirrors the former charly/status_nested_test.go cases, now expressed as the
// pre-resolved wire shape a real buildStatusRootsTree call would produce):
//   - root "check-nested-pod" (Kind pod) has two declared children:
//   - "worker": MatchKeys=[dotted, flattened]; a flat pod row exists at the DOTTED key
//     (Source "podman") → CLAIMED, real data inherited, removed from the top level.
//   - "worker-net": no flat row anywhere → SYNTHESIZED declared/nested.
//   - root "stack-vm" (Kind vm) has one declared child "web": MatchKeys=[dotted, flattened]; a
//     flat pod row exists at the FLATTENED key (NestedContainerName) → CLAIMED via the second
//     match-key candidate, proving the priority order.
//   - an unrelated flat row "redis" has no declared root → left untouched at the top level.
func TestOverlayGolden(t *testing.T) {
	const podParent = "check-nested-pod"
	const vmParent = "stack-vm"
	workerPath := podParent + ".worker"
	workerNetPath := podParent + ".worker-net"
	webDotted := vmParent + ".web"
	webFlat := kit.NestedContainerName(webDotted)

	rows := []spec.DeploymentStatus{
		{Kind: spec.SubstratePod, Image: podParent, Status: "running", Container: "charly-" + podParent, RunMode: "quadlet", Source: "podman"},
		{Kind: spec.SubstratePod, Image: workerPath, Status: "running", Container: "charly-worker", Network: "in-pod (charly-" + podParent + ")", RunMode: "quadlet", Source: "podman"},
		{Kind: spec.SubstrateVM, Image: vmParent, Status: "running", Container: "stack-vm", RunMode: "quadlet", Source: "libvirt"},
		{Kind: spec.SubstratePod, Image: webFlat, Status: "running", Uptime: "Up 3 minutes", Container: "charly-" + webFlat, RunMode: "quadlet", Source: "podman"},
		{Kind: spec.SubstratePod, Image: "redis", Status: "running", Container: "charly-redis", RunMode: "quadlet", Source: "podman"},
	}

	roots := []spec.StatusNestedNode{
		{
			Key: podParent, Path: podParent, Kind: spec.SubstratePod, HasChildren: true,
			MatchKeys: []string{podParent},
			Children: []*spec.StatusNestedNode{
				{Key: "worker", Path: workerPath, Kind: spec.SubstratePod, MatchKeys: []string{workerPath, kit.NestedContainerName(workerPath)}},
				{Key: "worker-net", Path: workerNetPath, Kind: spec.SubstratePod, MatchKeys: []string{workerNetPath, kit.NestedContainerName(workerNetPath)}},
			},
		},
		{
			Key: vmParent, Path: vmParent, Kind: spec.SubstrateVM, HasChildren: true,
			MatchKeys: []string{vmParent},
			Children: []*spec.StatusNestedNode{
				{Key: "web", Path: webDotted, Kind: spec.SubstratePod, MatchKeys: []string{webDotted, webFlat}},
			},
		},
	}

	out := applyNestedOverlay(rows, roots)

	var buf bytes.Buffer
	if err := RenderJSON(&buf, out); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	got := buf.String()
	t.Logf("overlay golden JSON:\n%s", got)

	const want = `[
  {
    "kind": "pod",
    "image": "check-nested-pod",
    "status": "running",
    "container": "charly-check-nested-pod",
    "run_mode": "quadlet",
    "nested": [
      {
        "kind": "pod",
        "image": "worker",
        "status": "running",
        "container": "charly-worker",
        "network": "in-pod (charly-check-nested-pod)",
        "run_mode": "quadlet",
        "source": "podman"
      },
      {
        "kind": "pod",
        "image": "worker-net",
        "status": "declared",
        "container": "",
        "run_mode": "quadlet",
        "source": "nested"
      }
    ],
    "source": "podman"
  },
  {
    "kind": "vm",
    "image": "stack-vm",
    "status": "running",
    "container": "stack-vm",
    "run_mode": "quadlet",
    "nested": [
      {
        "kind": "pod",
        "image": "web",
        "status": "running",
        "uptime": "Up 3 minutes",
        "container": "charly-stack-vm_web",
        "run_mode": "quadlet",
        "source": "podman"
      }
    ],
    "source": "libvirt"
  },
  {
    "kind": "pod",
    "image": "redis",
    "status": "running",
    "container": "charly-redis",
    "run_mode": "quadlet",
    "source": "podman"
  }
]
`

	if got != want {
		t.Errorf("overlay golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestOverlayGolden_NestedLiveStatusOverridesClaim verifies the --nested semantics in isolation:
// a claimed child's Status is UNCONDITIONALLY overwritten by its LiveStatus verdict (mirroring the
// former nestedChildStatus's unconditional overwrite under opts.Nested) — even though the claimed
// flat row says "running", the live probe's "unreachable" wins.
func TestOverlayGolden_NestedLiveStatusOverridesClaim(t *testing.T) {
	const parent = "stack-vm"
	webDotted := parent + ".web"
	webFlat := kit.NestedContainerName(webDotted)

	rows := []spec.DeploymentStatus{
		{Kind: spec.SubstrateVM, Image: parent, Status: "running", Container: "stack-vm", RunMode: "quadlet", Source: "libvirt"},
		{Kind: spec.SubstratePod, Image: webFlat, Status: "running", Container: "charly-" + webFlat, RunMode: "quadlet", Source: "podman"},
	}
	roots := []spec.StatusNestedNode{
		{
			Key: parent, Path: parent, Kind: spec.SubstrateVM, HasChildren: true,
			MatchKeys: []string{parent},
			Children: []*spec.StatusNestedNode{
				{Key: "web", Path: webDotted, Kind: spec.SubstratePod, MatchKeys: []string{webDotted, webFlat}, LiveStatus: "unreachable"},
			},
		},
	}

	out := applyNestedOverlay(rows, roots)
	if len(out) != 1 {
		t.Fatalf("top-level row count = %d, want 1 (web claimed+moved): %+v", len(out), out)
	}
	if len(out[0].Nested) != 1 {
		t.Fatalf("parent must carry 1 nested child, got %+v", out[0])
	}
	web := out[0].Nested[0]
	if web.Status != "unreachable" {
		t.Errorf("web.Status = %q, want %q (LiveStatus overrides the claimed row's real status)", web.Status, "unreachable")
	}
	// The claim still happened — real provenance data survives even though Status is overridden.
	if web.Source != "podman" {
		t.Errorf("web.Source = %q, want preserved %q", web.Source, "podman")
	}
	if web.Container != "charly-"+webFlat {
		t.Errorf("web.Container = %q, want moved %q", web.Container, "charly-"+webFlat)
	}
}

// TestOverlayGolden_NoParentRowNoPhantom verifies a declared parent with no flat row attaches
// nothing (no phantom parent row is synthesized) — the same invariant the former
// TestNestedOverlay_NoParentRowNoPhantom proved.
func TestOverlayGolden_NoParentRowNoPhantom(t *testing.T) {
	rows := []spec.DeploymentStatus{
		{Kind: spec.SubstratePod, Image: "redis", Status: "running", Container: "charly-redis", Source: "podman"},
	}
	roots := []spec.StatusNestedNode{
		{Key: "missing-parent", Path: "missing-parent", Kind: spec.SubstratePod, HasChildren: true, MatchKeys: []string{"missing-parent"},
			Children: []*spec.StatusNestedNode{{Key: "child", Path: "missing-parent.child", Kind: spec.SubstratePod, MatchKeys: []string{"missing-parent.child"}}}},
	}
	out := applyNestedOverlay(rows, roots)
	if len(out) != 1 || out[0].Image != "redis" || len(out[0].Nested) != 0 {
		t.Fatalf("no-parent-row overlay must be a no-op, got %+v", out)
	}
}

// TestOverlayGolden_EmptyRootsNoOp verifies applyNestedOverlay is a clean pass-through when the
// host resolved no declared tree at all.
func TestOverlayGolden_EmptyRootsNoOp(t *testing.T) {
	rows := []spec.DeploymentStatus{
		{Kind: spec.SubstratePod, Image: "redis", Status: "running", Container: "charly-redis", Source: "podman"},
	}
	out := applyNestedOverlay(rows, nil)
	if len(out) != 1 || out[0].Image != "redis" {
		t.Fatalf("empty-roots overlay must be a no-op, got %+v", out)
	}
}
