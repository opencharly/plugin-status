package status

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// nested_tree.go — the DECLARED nested-deployment tree pre-resolution, ported from
// charly/status_nested.go (K5). The former host header claimed this was CORE-COUPLED
// ("DeployConfig/ResolveDeployChain/NestedExecutor... a plugin cannot decode or dial") —
// that claim was STALE relative to what candy/plugin-substrate's own android status collector
// already proved: deploykit.LoadDeployConfig/MergeDeployConfigs/ClassifyTarget/ResolveDeployChain
// are ALL sdk-portable, so a plugin decodes + dials them exactly like the host did. The ONLY thing
// that genuinely could not cross the process boundary was never true — it was never attempted.
//
// buildStatusRootsTree resolves the declared tree (project, via InvokeProvider("build","project"),
// merged with the operator's per-host overlay via deploykit.LoadDeployConfig) into the wire-safe
// []spec.StatusNestedNode shape overlay.go's PURE fold (applyNestedOverlay) consumes.
//
// Member tree (spec #103 + sdk #221): the deploy node carries ONE ordered Member list
// (Name + Position + Node); the along-side-vs-into classification is DERIVED from the member's
// authored Position. This tree-builder consumes ONLY the InSubstrateMembers() — members deployed
// INTO their parent's venue, the ones addressed by a dotted path (parent.child) — because a
// deploy-level member is a folded top-level addressable Deploy entry at load (it surfaces as its
// own root here, never re-nested under its owner), the SAME convention WalkDeploymentTree
// (deploykit/deploy_tree.go) walks by.

// nestedProbeTimeout bounds the per-child live probe under --nested. A child whose multi-hop venue
// doesn't answer within this window renders Status:"unreachable" instead of blocking the whole
// table. A context DEADLINE, never a sleep/retry loop (CLAUDE.md R4).
const nestedProbeTimeout = 4 * time.Second

// resolvedProject fetches the resolved-project envelope over the reverse channel — the same
// InvokeProvider("build","project") seam candy/plugin-check and candy/plugin-substrate already
// consume. The host's resolveProjectEnvelope (plugin-build) caches the result persistently, so
// this is a plain forward.
func resolvedProject(ex *sdk.Executor, ctx context.Context) (*spec.ResolvedProject, error) {
	reqJSON, err := json.Marshal(spec.ResolvedProjectRequest{Dir: ""})
	if err != nil {
		return nil, err
	}
	out, err := ex.InvokeProvider(ctx, "build", "project", sdk.OpResolve, reqJSON, nil, sdk.InvokeProviderOpts{})
	if err != nil {
		return nil, err
	}
	var rp spec.ResolvedProject
	if uerr := json.Unmarshal(out, &rp); uerr != nil {
		return nil, uerr
	}
	return &rp, nil
}

func buildStatusRootsTree(ex *sdk.Executor, ctx context.Context, nested bool) ([]spec.StatusNestedNode, error) {
	rawRoots, err := mergedNestedRoots(ex, ctx)
	if err != nil {
		return nil, err
	}
	return buildStatusRootsTreeFrom(rawRoots, nested), nil
}

// buildStatusRootsTreeFrom is the PURE tree-assembly step: every decision (which kind a node is,
// which flat-row keys index it, and — under nested — its live-probe verdict) is made HERE, given
// an already-merged roots map. Only roots WITH in-substrate members are emitted (the pure overlay
// skips a childless root anyway); a root carrying ONLY deploy-level members is childless in the
// nested sense — those members are their own top-level Deploy entries.
func buildStatusRootsTreeFrom(rawRoots map[string]deploykit.DeployNode, nested bool) []spec.StatusNestedNode {
	if len(rawRoots) == 0 {
		return nil
	}
	var out []spec.StatusNestedNode
	for _, key := range sortedRootKeys(rawRoots) {
		root := rawRoots[key]
		children := buildStatusChildNodes(key, &root, rawRoots, nested)
		if len(children) == 0 {
			continue
		}
		out = append(out, spec.StatusNestedNode{
			Key:         key,
			Path:        key,
			Kind:        nestedChildKind(&root),
			HasChildren: true,
			MatchKeys:   []string{key},
			Children:    children,
		})
	}
	return out
}

// buildStatusChildNodes recurses buildStatusRootsTree's per-root walk into the IN-SUBSTRATE
// members of parentNode (at dotted path parentPath), in the member tree's authored order —
// the ordered Member list replaces the former sorted Children map. Each child's MatchKeys
// carries BOTH candidate flat-row keys in the SAME priority order the pure overlay's
// claimFlatRow tries them (dotted path first, then the flattened NestedContainerName).
func buildStatusChildNodes(parentPath string, parentNode *deploykit.DeployNode, rawRoots map[string]deploykit.DeployNode, nested bool) []*spec.StatusNestedNode {
	members := parentNode.InSubstrateMembers()
	if len(members) == 0 {
		return nil
	}

	out := make([]*spec.StatusNestedNode, 0, len(members))
	for _, m := range members {
		if m.Node == nil {
			continue
		}
		child := m.Node
		k := m.Name
		childPath := parentPath + "." + k
		node := &spec.StatusNestedNode{
			Key:         k,
			Path:        childPath,
			Kind:        nestedChildKind(child),
			HasChildren: child.HasMembers() && len(child.InSubstrateMembers()) > 0,
			MatchKeys:   []string{childPath, kit.NestedContainerName(childPath)},
			Children:    buildStatusChildNodes(childPath, child, rawRoots, nested),
		}
		if nested {
			node.LiveStatus = probeNestedChildLive(childPath, rawRoots)
		}
		out = append(out, node)
	}
	return out
}

// probeNestedChildLive resolves the dotted path to a DeployExecutor chain and runs a trivial
// liveness probe under nestedProbeTimeout. Returns "reachable" on a clean exit, "unreachable" on
// any error / non-zero exit / timeout. The chain construction reuses deploykit.ResolveDeployChain —
// the SAME primitive `charly deploy` and `charly check live parent.child` use (R3).
func probeNestedChildLive(childPath string, roots map[string]deploykit.DeployNode) string {
	leaf, chain, err := deploykit.ResolveDeployChain(roots, childPath, nil)
	if err != nil || chain == nil || leaf == nil {
		return "unreachable"
	}
	pctx, cancel := context.WithTimeout(context.Background(), nestedProbeTimeout)
	defer cancel()
	_, _, exit, perr := chain.RunCapture(pctx, "true")
	if perr != nil || exit != 0 {
		return "unreachable"
	}
	return "reachable"
}

// nestedChildKind maps a nested node's target to the SubstrateKind used for the row's KIND cell.
// deploykit.ClassifyTarget normalizes empty/legacy spellings, so pod/vm/kubernetes/local/android all
// resolve to their canonical kind.
func nestedChildKind(child *deploykit.DeployNode) spec.SubstrateKind {
	switch deploykit.ClassifyTarget(child) {
	case "vm":
		return spec.SubstrateVM
	case "kubernetes":
		return spec.SubstrateKubernetes
	case "local", "host":
		return spec.SubstrateLocal
	case "android":
		return spec.SubstrateAndroid
	default:
		return spec.SubstratePod
	}
}

// loadDeployConfig reads the per-host deploy overlay (~/.config/charly/charly.yml) via the
// cycle-free plugin-side helper loaderkit.LoadHostDeployConfigViaExecutor (#55 coneC Unit C2 —
// this retired the former deploykit.LoadDeployConfigViaSeam host-handler round-trip;
// loaderkit already imports deploykit so the
// helper lives there and a plugin calls it directly, placement-invariant — the bare
// deploykit.LoadDeployConfig silently no-ops outside charly-core's own init() since
// deploykit.DeployStateHost is only ever registered there, never by an out-of-process plugin
// process). R3 hoist (charly#176 round 1): the former LoadDeployConfigViaSeam itself hoisted four
// near-identical local copies (candy/plugin-substrate's status_flat.go,
// candy/plugin-fleet/ephemeral.go, candy/plugin-pod/remove_orchestration.go, this one); the C2
// helper is now the ONE shared implementation all four call. Returns (nil, nil) on an
// absent/empty overlay, matching deploykit.LoadDeployConfig's own contract.
func loadDeployConfig(ex *sdk.Executor, ctx context.Context) (*deploykit.DeployConfig, error) {
	// Direct read of the per-host config — a lightweight yaml.Unmarshal into the
	// DeployConfig, NOT the full LoadUnified project walk (which allocates ~197MB
	// per load — the GC pressure that dominated `charly status`). The per-host
	// config is a small file with no imports; the direct read is
	// placement-invariant (the file is on the same host).
	path, err := spec.DefaultDeployConfigPath()
	if err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dc deploykit.DeployConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, err
	}
	return &dc, nil
}

// mergedNestedRoots returns the declared deployment tree (project + per-machine overlay) — the
// I/O half: fetch the project envelope + the operator's per-host overlay, then hand off to the
// PURE mergedNestedRootsFrom. Mirrors candy/plugin-substrate's status_android_collect.go split
// (fetchResolvedProject + loadDeployConfig in the outer function, collectAndroidDeployNodes as the
// pure plain-parameter function) exactly, R3.
func mergedNestedRoots(ex *sdk.Executor, ctx context.Context) (map[string]deploykit.DeployNode, error) {
	rp, err := resolvedProject(ex, ctx)
	if err != nil {
		return nil, err
	}
	// Best-effort: absence of a per-machine overlay is normal (mirrors
	// candy/plugin-substrate's newFlatCollector, K6, the same pattern).
	perMachine, _ := loadDeployConfig(ex, ctx)
	return mergedNestedRootsFrom(rp, perMachine), nil
}

// mergedNestedRootsFrom is the PURE merge step: project deploy tree (project then local overlay
// wins per key, deploykit.MergeDeployConfigs — the SAME precedence the merged-tree read uses),
// callable directly from a test with in-memory fixtures (no LoadDeployConfig I/O).
func mergedNestedRootsFrom(rp *spec.ResolvedProject, perMachine *deploykit.DeployConfig) map[string]deploykit.DeployNode {
	projectFleet := make(map[string]deploykit.DeployNode, len(rp.Deploy))
	for name, node := range rp.Deploy {
		if node != nil {
			projectFleet[name] = deploykit.DeployNode(*node)
		}
	}
	merged := deploykit.MergeDeployConfigs(&deploykit.DeployConfig{Deploy: projectFleet}, perMachine)
	if merged == nil {
		return nil
	}
	return merged.Deploy
}

func sortedRootKeys(roots map[string]deploykit.DeployNode) []string {
	keys := make([]string, 0, len(roots))
	for k := range roots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
