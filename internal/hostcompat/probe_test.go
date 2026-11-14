package hostcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwpop"
)

// fakeHost answers the GWP calls the probe makes with canned data, and records
// every method it was asked for so a test can pin the probe's read surface.
type fakeHost struct {
	properties map[string]any // "<selector> <property>" -> value
	find       map[string]string
	tree       string
	gameState  string
	metrics    string
	calls      []string
}

func (f *fakeHost) Call(_ context.Context, method string, params any) (*godotconn.Response, error) {
	f.calls = append(f.calls, method)
	p, _ := params.(map[string]any)
	var body string
	switch method {
	case "ping":
		body = `{"status":"ok","engine_version":"4.6.2-stable","stagehand_version":"0.5.2","protocol_version":"1.0"}`
	case "get_property":
		key := fmt.Sprintf("%v %v", p["selector"], p["property"])
		value, ok := f.properties[key]
		if !ok {
			return nil, fmt.Errorf("fakeHost: no property %q", key)
		}
		raw, _ := json.Marshal(map[string]any{"value": value})
		body = string(raw)
	case "query_nodes":
		selector, _ := p["selector"].(string)
		body = f.find[selector]
		if body == "" {
			return nil, fmt.Errorf("fakeHost: no find %q", selector)
		}
	case "get_tree":
		body = f.tree
	case "get_game_state":
		body = f.gameState
	case "get_performance":
		body = f.metrics
	default:
		return nil, fmt.Errorf("fakeHost: unexpected method %q", method)
	}
	return &godotconn.Response{Result: json.RawMessage(body)}, nil
}

func baselineHost() *fakeHost {
	return &fakeHost{
		properties: map[string]any{
			"/root content_scale_factor": 1.5,
			"/root content_scale_mode":   1,
			"/root content_scale_aspect": 4,
			"/root content_scale_size":   map[string]any{"x": 1152, "y": 648},
			"/root gui_embed_subwindows": true,
		},
		find: map[string]string{
			"class:Window": `{"count":3,"nodes":[
				{"name":"root","class":"Window","path":"/root","properties":{"visible":true}},
				{"name":"Dialog","class":"AcceptDialog","path":"/root/Main/Dialog","properties":{"visible":false}},
				{"name":"Popup","class":"PopupMenu","path":"/root/Main/Popup","properties":{"visible":true}}]}`,
			"group:_stagehand_protected": `{"count":1,"nodes":[{"name":"StagehandServer","class":"Node","path":"/root/StagehandServer"}]}`,
		},
		tree: `{"name":"root","class":"Window","path":"/root","children":[
			{"name":"Settings","class":"Node","path":"/root/Settings","properties":{}},
			{"name":"StagehandServer","class":"Node","path":"/root/StagehandServer"},
			{"name":"Bus","class":"Node","path":"/root/Bus","properties":{}},
			{"name":"Main","class":"Control","path":"/root/Main","properties":{"scene_file_path":"res://main.tscn"}}]}`,
		gameState: `{"current_scene":"res://main.tscn","fps":60,"paused":false}`,
		metrics:   `{"metrics":{"OBJECT_NODE_COUNT":42}}`,
	}
}

func TestCollectReportsEveryField(t *testing.T) {
	host := baselineHost()
	got, err := Collect(context.Background(), host)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got.Engine != "4.6.2-stable" || got.Addon != "0.5.2" {
		t.Errorf("engine/addon = %q/%q", got.Engine, got.Addon)
	}
	cs := got.ContentScale
	if cs.Factor != 1.5 || cs.Mode != "canvas_items" || cs.Aspect != "expand" || cs.Size != (Size{X: 1152, Y: 648}) {
		t.Errorf("content scale = %+v", cs)
	}
	// Stagehand's own autoload and the current scene are not the host's autoloads.
	if want := []string{"Settings", "Bus"}; !slices.Equal(got.Autoloads, want) {
		t.Errorf("autoloads = %v, want %v", got.Autoloads, want)
	}
	if got.AutoloadCount != 2 {
		t.Errorf("autoload count = %d", got.AutoloadCount)
	}
	if !got.GUIEmbedSubwindows || len(got.Windows) != 2 {
		t.Errorf("windows = %+v embed=%v (root must not count as a subwindow)", got.Windows, got.GUIEmbedSubwindows)
	}
	if !got.EmbeddedSubwindows {
		t.Error("embedded_subwindows = false, want true")
	}
	if got.PausedAtBoot == nil || *got.PausedAtBoot {
		t.Errorf("paused = %v, want false", got.PausedAtBoot)
	}
	if got.NodeCount != 42 {
		t.Errorf("node count = %d", got.NodeCount)
	}
	want := []string{AxisEmbeddedSubwindows, AxisContentScale} // knownAxes order
	if !slices.Equal(got.Axes, want) {
		t.Errorf("axes = %v, want %v", got.Axes, want)
	}
}

func TestCollectTreatsAddonWithoutPausedFieldAsUnknown(t *testing.T) {
	host := baselineHost()
	host.gameState = `{"current_scene":"res://main.tscn","fps":60}`
	got, err := Collect(context.Background(), host)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// An addon that predates the field cannot say. "unknown" must not read as
	// "not paused", or an old host silently loses its paused-tree axis.
	if got.PausedAtBoot != nil {
		t.Errorf("paused = %v, want nil (unknown)", *got.PausedAtBoot)
	}
	if slices.Contains(got.Axes, AxisPausedTree) {
		t.Errorf("axes %v must not claim paused-tree from an unknown", got.Axes)
	}
}

func TestCollectDerivesPausedAndHeavyAutoloadAxes(t *testing.T) {
	host := baselineHost()
	host.gameState = `{"current_scene":"res://main.tscn","paused":true}`
	var children string
	for i := 0; i < HeavyAutoloadThreshold; i++ {
		children += fmt.Sprintf(`{"name":"A%d","class":"Node","path":"/root/A%d","properties":{}},`, i, i)
	}
	host.tree = `{"name":"root","class":"Window","path":"/root","children":[` + children +
		`{"name":"Main","class":"Control","path":"/root/Main","properties":{"scene_file_path":"res://main.tscn"}}]}`

	got, err := Collect(context.Background(), host)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, axis := range []string{AxisPausedTree, AxisHeavyAutoloads} {
		if !slices.Contains(got.Axes, axis) {
			t.Errorf("axes %v missing %s", got.Axes, axis)
		}
	}
}

func TestCollectPlainHostClaimsNoAxes(t *testing.T) {
	host := baselineHost()
	host.properties["/root content_scale_factor"] = 1.0
	host.properties["/root content_scale_mode"] = 0
	host.properties["/root gui_embed_subwindows"] = false
	got, err := Collect(context.Background(), host)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got.Axes) != 0 {
		t.Errorf("axes = %v, want none", got.Axes)
	}
	if got.ContentScale.Mode != "disabled" {
		t.Errorf("mode = %q", got.ContentScale.Mode)
	}
}

func TestCollectNamesUnknownScaleEnums(t *testing.T) {
	host := baselineHost()
	host.properties["/root content_scale_mode"] = 9
	got, err := Collect(context.Background(), host)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.ContentScale.Mode != "unknown(9)" {
		t.Errorf("mode = %q, want unknown(9)", got.ContentScale.Mode)
	}
}

// The probe is a reporter, so it may read more than a host scenario may assert
// (see surface.go) — but never via the unsafe methods, which would need
// allow_unsafe against code we did not write. This test pins both halves of
// that distinction so neither can drift silently.
func TestProbeReadsStayOutsideTheAssertionSurfaceAndInsideSafeMode(t *testing.T) {
	var outsideSurface int
	for _, action := range ProbeActions() {
		spec, ok := gwpop.Lookup(action)
		if !ok {
			t.Fatalf("probe action %q is not a gwpop action", action)
		}
		if spec.Capability == gwp.CapabilityUnsafe {
			t.Errorf("probe action %q needs the unsafe capability; the probe must work in safe mode", action)
		}
		if !slices.Contains(AllowedActions(), action) {
			outsideSurface++
		}
	}
	if outsideSurface == 0 {
		t.Error("the probe reads nothing beyond the assertion surface; the reporter/assertion split is meaningless")
	}

	host := baselineHost()
	if _, err := Collect(context.Background(), host); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, method := range host.calls {
		if method == "evaluate" || method == "call_method" {
			t.Errorf("probe called unsafe method %q", method)
		}
	}
}

func TestCollectSurfacesAReadFailure(t *testing.T) {
	host := baselineHost()
	delete(host.properties, "/root content_scale_factor")
	if _, err := Collect(context.Background(), host); err == nil {
		t.Fatal("Collect succeeded without being able to read content_scale_factor")
	}
}
