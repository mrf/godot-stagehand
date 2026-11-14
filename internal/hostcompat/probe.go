package hostcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/mrf/godot-stagehand/internal/gwpop"
)

// The capability probe (design doc Amendment 7a): one routine, identical for
// every host, that connects and reports what the host actually is. Its output
// populates a manifest entry's axes, so onboarding a project is "run the probe,
// commit what it found" rather than reading project.godot and guessing —
// content-scale is the proof that guessing is wrong, because Pixelorama sets
// it from script and project.godot shows nothing.
//
// REPORTER, NOT ASSERTION. surface.go confines a host *scenario* to the
// actions that prove the mechanism works. The probe is the opposite kind of
// thing: it observes the host and asserts nothing, so it reads more than that
// allowlist permits (get_property, game_state, find). The line it must not
// cross is the unsafe capability — no evaluate, no call_method — so the probe
// still works against a host launched in safe mode. Both halves are pinned by
// TestProbeReadsStayOutsideTheAssertionSurfaceAndInsideSafeMode.
//
// Which GWP method reads which fact (verified against the addon, not assumed):
//
//	content scale      get_property on /root (a Window): content_scale_factor,
//	                   content_scale_mode, content_scale_aspect, content_scale_size
//	embedded windows   get_property /root gui_embed_subwindows, plus find class:Window
//	paused tree        game_state.paused. SceneTree.paused is not a node property
//	                   and the only other readers (evaluate, call_method) are
//	                   unsafe-gated, so the addon reports it on get_game_state.
//	autoloads          tree /root depth 1, minus the current scene and Stagehand
//	node count         get_performance OBJECT_NODE_COUNT
//
// The time-to-ready measurement is taken around the launch, not by a GWP call;
// see LaunchAndProbe.

// HeavyAutoloadThreshold is the autoload count at which a host earns the
// heavy-autoloads axis. The manifest's evidence brackets it: Pixelorama's 12
// qualifies, Source of Mana's 4 does not (Amendment 8 / manifest notes). The
// number in between is a judgement, not a measurement — revisit it when a host
// lands in the gap.
const HeavyAutoloadThreshold = 8

// protectedGroup is the group Stagehand's own autoload joins
// (addons/stagehand/core/self_protection.gd PROTECTED_GROUP).
const protectedGroup = "_stagehand_protected"

// Window.ContentScaleMode and Window.ContentScaleAspect, verified against
// Godot 4.6.2 by printing the constants. A value outside the table is
// reported as "unknown(N)" rather than guessed.
var (
	contentScaleModes   = []string{"disabled", "canvas_items", "viewport"}
	contentScaleAspects = []string{"ignore", "keep", "keep_width", "keep_height", "expand"}
)

// Size is an integer 2-vector.
type Size struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ContentScale is /root's runtime scaling configuration.
type ContentScale struct {
	Factor float64 `json:"factor"`
	Mode   string  `json:"mode"`
	Aspect string  `json:"aspect"`
	Size   Size    `json:"size"`
}

// Active reports whether scaling is in effect: the stretch mode is on, or the
// factor is not the identity. Either one makes node rects and window
// coordinates disagree, which is what the content-scale axis stresses.
func (c ContentScale) Active() bool { return c.Mode != "disabled" || c.Factor != 1 }

// Window is a Window node below /root.
type Window struct {
	Path    string `json:"path"`
	Class   string `json:"class"`
	Visible bool   `json:"visible"`
}

// Probe is what a host turned out to be. Field names are the output contract
// of `godot-stagehand hostcompat-probe`.
type Probe struct {
	Engine string `json:"engine"`
	Addon  string `json:"addon"`

	ContentScale ContentScale `json:"content_scale"`

	// Autoloads are /root's children other than the current scene and
	// Stagehand's own. It is a runtime observation, so a host that parents
	// extra nodes to /root by hand is counted too; that is the honest cost of
	// not reading project.godot.
	Autoloads     []string `json:"autoloads"`
	AutoloadCount int      `json:"autoload_count"`

	// Windows are the non-root Window nodes (hidden dialogs included: they
	// embed the moment they are shown). GUIEmbedSubwindows is /root's own
	// switch for whether such windows embed or become OS windows.
	Windows            []Window `json:"windows"`
	GUIEmbedSubwindows bool     `json:"gui_embed_subwindows"`
	EmbeddedSubwindows bool     `json:"embedded_subwindows"`

	// PausedAtBoot is nil when the addon cannot say (it predates the
	// game_state.paused field). It is the tree's state at probe time, which is
	// the first moment a client can observe it; a host that pauses only during
	// a splash and unpauses before we connect is invisible here.
	PausedAtBoot *bool `json:"paused_at_boot"`

	// NodeCount is Performance.OBJECT_NODE_COUNT, so it includes Stagehand's
	// own nodes.
	NodeCount int `json:"node_count"`

	// TimeToReadyMs is wall-clock from starting the launch to the addon
	// answering its first ping, which cannot happen before the first frame
	// (the addon accepts connections from _process). ColdImport records that
	// res://.godot/imported did not exist beforehand, so the number includes
	// the one-off asset import — the part that makes a project large.
	// Both are zero when Collect was called on an already-connected session.
	TimeToReadyMs int64 `json:"time_to_ready_ms"`
	ColdImport    bool  `json:"cold_import"`

	// Axes are the stress axes this run observed, in knownAxes order. An axis
	// absent here was either not present or not observable by a probe; see
	// Unprobed.
	Axes []string `json:"axes"`
	// Unprobed are the axes no runtime read can settle, so the probe makes no
	// claim about them either way. They stay hand-verified in the manifest.
	Unprobed []string `json:"unprobed"`
}

// unprobedAxes are the known axes the probe cannot observe. large-project is
// here on purpose: the probe reports the measurements (TimeToReadyMs,
// ColdImport) but where "large" starts is a budget decision, not a fact.
var unprobedAxes = []string{
	AxisOwnCLIParser, AxisCustomInput, AxisLargeProject, AxisHostStrictWarnings,
}

// probeActions are every gwpop action Collect issues, in call order.
var probeActions = []string{"ping", "get_property", "game_state", "tree", "find", "get_performance"}

// ProbeActions returns the gwpop actions the probe reads through.
func ProbeActions() []string { return slices.Clone(probeActions) }

// Collect reads the host's capabilities over an established session. It
// returns an error rather than a partial report when any read fails: a probe
// that silently omits a field would populate a manifest with a false negative,
// which is the failure the probe exists to remove.
func Collect(ctx context.Context, c gwpop.Caller) (*Probe, error) {
	r := reader{ctx: ctx, c: c}
	p := &Probe{}

	var ping struct {
		Engine string `json:"engine_version"`
		Addon  string `json:"stagehand_version"`
	}
	r.do("ping", nil, &ping)
	p.Engine, p.Addon = ping.Engine, ping.Addon

	if err := r.contentScale(p); err != nil {
		return nil, err
	}
	p.GUIEmbedSubwindows = r.rootBool("gui_embed_subwindows")

	var state struct {
		CurrentScene string `json:"current_scene"`
		Paused       *bool  `json:"paused"`
	}
	r.do("game_state", nil, &state)
	p.PausedAtBoot = state.Paused

	r.autoloads(p, state.CurrentScene)
	r.windows(p)

	var perf struct {
		Metrics map[string]float64 `json:"metrics"`
	}
	r.do("get_performance", map[string]any{"monitors": []string{"OBJECT_NODE_COUNT"}}, &perf)
	p.NodeCount = int(perf.Metrics["OBJECT_NODE_COUNT"])

	if r.err != nil {
		return nil, r.err
	}
	p.EmbeddedSubwindows = p.GUIEmbedSubwindows && len(p.Windows) > 0
	p.Axes = p.observedAxes()
	p.Unprobed = slices.Clone(unprobedAxes)
	return p, nil
}

func (p *Probe) observedAxes() []string {
	observed := map[string]bool{
		AxisContentScale:       p.ContentScale.Active(),
		AxisEmbeddedSubwindows: p.EmbeddedSubwindows,
		AxisPausedTree:         p.PausedAtBoot != nil && *p.PausedAtBoot,
		AxisHeavyAutoloads:     p.AutoloadCount >= HeavyAutoloadThreshold,
	}
	axes := []string{}
	for _, axis := range knownAxes {
		if observed[axis] {
			axes = append(axes, axis)
		}
	}
	return axes
}

// reader runs probe reads and remembers the first failure, so Collect reads as
// a list of facts instead of an error ladder.
type reader struct {
	ctx context.Context
	c   gwpop.Caller
	err error
}

func (r *reader) do(action string, params map[string]any, into any) {
	if r.err != nil {
		return
	}
	raw, err := gwpop.Execute(r.ctx, r.c, gwpop.Op{Action: action, Params: params})
	if err != nil {
		r.err = fmt.Errorf("probe %s: %w", action, err)
		return
	}
	if err := json.Unmarshal(raw, into); err != nil {
		r.err = fmt.Errorf("probe %s: decode %s: %w", action, raw, err)
	}
}

func (r *reader) rootProperty(property string, into any) {
	var res struct {
		Value json.RawMessage `json:"value"`
	}
	r.do("get_property", map[string]any{"selector": "/root", "property": property}, &res)
	if r.err != nil {
		return
	}
	if err := json.Unmarshal(res.Value, into); err != nil {
		r.err = fmt.Errorf("probe get_property /root %s: decode %s: %w", property, res.Value, err)
	}
}

func (r *reader) rootBool(property string) bool {
	var v bool
	r.rootProperty(property, &v)
	return v
}

func (r *reader) contentScale(p *Probe) error {
	var mode, aspect int
	r.rootProperty("content_scale_factor", &p.ContentScale.Factor)
	r.rootProperty("content_scale_mode", &mode)
	r.rootProperty("content_scale_aspect", &aspect)
	r.rootProperty("content_scale_size", &p.ContentScale.Size)
	p.ContentScale.Mode = enumName(contentScaleModes, mode)
	p.ContentScale.Aspect = enumName(contentScaleAspects, aspect)
	return r.err
}

func enumName(names []string, value int) string {
	if value < 0 || value >= len(names) {
		return fmt.Sprintf("unknown(%d)", value)
	}
	return names[value]
}

type nodeInfo struct {
	Name       string `json:"name"`
	Class      string `json:"class"`
	Path       string `json:"path"`
	Properties struct {
		SceneFilePath string `json:"scene_file_path"`
		Visible       *bool  `json:"visible"`
	} `json:"properties"`
}

// protectedPaths are Stagehand's own nodes, which every count must exclude.
func (r *reader) protectedPaths() []string {
	var res struct {
		Nodes []nodeInfo `json:"nodes"`
	}
	r.do("find", map[string]any{"selector": "group:" + protectedGroup}, &res)
	paths := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		paths = append(paths, n.Path)
	}
	return paths
}

func (r *reader) autoloads(p *Probe, currentScene string) {
	var tree struct {
		Children []nodeInfo `json:"children"`
	}
	r.do("tree", map[string]any{
		"root_path":  "/root",
		"max_depth":  1,
		"properties": []string{"scene_file_path"},
	}, &tree)
	own := r.protectedPaths()

	p.Autoloads = []string{}
	sceneSkipped := false
	for _, child := range tree.Children {
		if slices.Contains(own, child.Path) {
			continue
		}
		// The current scene is the one child whose scene file is the current
		// scene. Match the first only: an autoload can be that same scene.
		if !sceneSkipped && currentScene != "" && child.Properties.SceneFilePath == currentScene {
			sceneSkipped = true
			continue
		}
		p.Autoloads = append(p.Autoloads, child.Name)
	}
	p.AutoloadCount = len(p.Autoloads)
}

func (r *reader) windows(p *Probe) {
	var res struct {
		Nodes []nodeInfo `json:"nodes"`
	}
	r.do("find", map[string]any{
		"selector":   "class:Window",
		"properties": []string{"visible"},
		"limit":      500,
	}, &res)
	p.Windows = []Window{}
	for _, n := range res.Nodes {
		if n.Path == "/root" {
			continue
		}
		p.Windows = append(p.Windows, Window{
			Path: n.Path, Class: n.Class,
			Visible: n.Properties.Visible != nil && *n.Properties.Visible,
		})
	}
}
