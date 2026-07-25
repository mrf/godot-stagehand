package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/scenario"
	"github.com/mrf/godot-stagehand/internal/version"
	"github.com/mrf/godot-stagehand/internal/visual"
)

// ── connect / status ──────────────────────────────────────────────────────

var cmdConnect = &command{
	name: "connect", usage: "--port N [--token T]", connects: true,
	summary: "Verify a connection to a running game and print its handshake",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		return runWithFlags(ctx, cmd, e, args, nil, func(_ context.Context, s *session, _ *flag.FlagSet) error {
			return emit(e.stdout, handshakeReport(s))
		})
	},
}

var cmdStatus = &command{
	name: "status", usage: "--port N [--token T]", connects: true,
	summary: "Report connection status and live game state",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		return runWithFlags(ctx, cmd, e, args, nil, func(ctx context.Context, s *session, _ *flag.FlagSet) error {
			state, err := gwpop.Execute(ctx, s.Caller(), gwpop.Op{Action: "game_state"})
			if err != nil {
				return err
			}
			report := handshakeReport(s)
			report["game_state"] = rawOrString(state)
			return emit(e.stdout, report)
		})
	},
}

func handshakeReport(s *session) map[string]any {
	report := map[string]any{
		"connected":         true,
		"host":              s.host,
		"port":              s.port,
		"cli_version":       version.Version,
		"protocol":          gwp.ProtocolID,
		"protocol_version":  s.handshake.ProtocolVersion,
		"engine_version":    s.handshake.EngineVersion,
		"stagehand_version": s.handshake.StagehandVersion,
		"capabilities":      s.handshake.Capabilities,
	}
	if len(s.handshake.MissingOptional) > 0 {
		report["missing_optional_capabilities"] = s.handshake.MissingOptional
		report["warning"] = s.handshake.Summary()
	}
	return report
}

// ── inspection ────────────────────────────────────────────────────────────

var cmdTree = &command{
	name: "tree", usage: "--port N [--root-path P] [--max-depth D]", connects: true,
	summary: "Snapshot the scene tree",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var rootPath, properties string
		var maxDepth int
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&rootPath, "root-path", "", "subtree root (default /root)")
			fset.IntVar(&maxDepth, "max-depth", 0, "maximum recursion depth (default 10)")
			fset.StringVar(&properties, "properties", "", "comma-separated properties to include per node")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			params := map[string]any{}
			setIf(params, "root_path", rootPath, wasSet(fset, "root-path"))
			setIf(params, "max_depth", maxDepth, wasSet(fset, "max-depth"))
			if props := splitList(properties); len(props) > 0 {
				params["properties"] = props
			}
			return execute(ctx, e, s, "tree", params)
		})
	},
}

var cmdFind = &command{
	name: "find", usage: "--port N <selector>", connects: true,
	summary: "Find nodes matching a selector",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var properties string
		var limit int
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&properties, "properties", "", "comma-separated properties to return per node")
			fset.IntVar(&limit, "limit", 0, "maximum results (default 50)")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 1, "<selector>"); err != nil {
				return err
			}
			params := map[string]any{"selector": fset.Arg(0)}
			setIf(params, "limit", limit, wasSet(fset, "limit"))
			if props := splitList(properties); len(props) > 0 {
				params["properties"] = props
			}
			return execute(ctx, e, s, "find", params)
		})
	},
}

// ── property ──────────────────────────────────────────────────────────────

var cmdProperty = &command{
	name: "property", usage: "get|set --port N <selector> <property> [value]", connects: true,
	summary: "Read or write a node property",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		if len(args) == 0 {
			return usagef(fmt.Errorf("property needs a subcommand: get or set"))
		}
		sub := args[0]
		if sub != "get" && sub != "set" {
			return usagef(fmt.Errorf("unknown property subcommand %q (want get or set)", sub))
		}
		return runWithFlags(ctx, cmd, e, args[1:], nil, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if sub == "get" {
				if err := requireArgs(cmd, fset.Args(), 2, "<selector> <property>"); err != nil {
					return err
				}
				return execute(ctx, e, s, "get_property", map[string]any{
					"selector": fset.Arg(0), "property": fset.Arg(1),
				})
			}
			if err := requireArgs(cmd, fset.Args(), 3, "<selector> <property> <value>"); err != nil {
				return err
			}
			return execute(ctx, e, s, "set_property", map[string]any{
				"selector": fset.Arg(0), "property": fset.Arg(1), "value": parseValue(fset.Arg(2)),
			})
		})
	},
}

// ── call / eval / scene ───────────────────────────────────────────────────

var cmdCall = &command{
	name: "call", usage: "--port N <selector> <method> [--args '[1,\"two\"]']", connects: true,
	summary: "Call a method on a node",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var argsJSON string
		var allowMultiple bool
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&argsJSON, "args", "", "JSON array of method arguments")
			fset.BoolVar(&allowMultiple, "allow-multiple", false, "call on every matched node")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 2, "<selector> <method>"); err != nil {
				return err
			}
			params := map[string]any{"selector": fset.Arg(0), "method": fset.Arg(1)}
			if argsJSON != "" {
				parsed, err := parseJSONArray("args", argsJSON)
				if err != nil {
					return err
				}
				params["args"] = parsed
			}
			setIf(params, "allow_multiple", allowMultiple, allowMultiple)
			return execute(ctx, e, s, "call_method", params)
		})
	},
}

var cmdEval = &command{
	name: "eval", usage: "--port N <expression>", connects: true,
	summary: "Evaluate a GDScript expression (needs a game launched with unsafe methods enabled)",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var contextNode string
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&contextNode, "context-node", "", "node path to use as 'self'")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 1, "<expression>"); err != nil {
				return err
			}
			params := map[string]any{"expression": fset.Arg(0)}
			setIf(params, "context_node", contextNode, contextNode != "")
			return execute(ctx, e, s, "evaluate", params)
		})
	},
}

var cmdScene = &command{
	name: "scene", usage: "--port N <scene_path>", connects: true,
	summary: "Change to a different scene",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		return runWithFlags(ctx, cmd, e, args, nil, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if err := requireArgs(cmd, fset.Args(), 1, "<scene_path>"); err != nil {
				return err
			}
			return execute(ctx, e, s, "change_scene", map[string]any{"scene_path": fset.Arg(0)})
		})
	},
}

// ── input ─────────────────────────────────────────────────────────────────

var cmdInput = &command{
	name: "input", usage: "click|key|action|text|move|touch --port N [...]", connects: true,
	summary: "Simulate input: click, key, action, text, move, touch",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		if len(args) == 0 {
			return usagef(fmt.Errorf("input needs a subcommand: click, key, action, text, move or touch"))
		}
		sub, rest := args[0], args[1:]

		var selector, position, coordinates, dragTo, button, modifiers, touchAction string
		var doubleClick bool
		var holdMs, delayMs, durationMs, touchIndex int
		var strength float64
		bind := func(fset *flag.FlagSet) {
			switch sub {
			case "click":
				fset.StringVar(&selector, "selector", "", "node to click")
				fset.StringVar(&position, "position", "", "screen coordinates as JSON, e.g. {\"x\":10,\"y\":20}")
				fset.StringVar(&button, "button", "", "mouse button: left, right, middle")
				fset.BoolVar(&doubleClick, "double", false, "double-click")
			case "key":
				fset.StringVar(&modifiers, "modifiers", "", "comma-separated: shift,ctrl,alt,meta")
				fset.IntVar(&holdMs, "hold-ms", 0, "how long to hold the key")
			case "action":
				fset.Float64Var(&strength, "strength", 0, "action strength 0.0-1.0")
				fset.IntVar(&holdMs, "hold-ms", 0, "how long to hold the action")
			case "text":
				fset.StringVar(&selector, "selector", "", "node to focus first")
				fset.IntVar(&delayMs, "delay-ms", 0, "delay between characters")
			case "move":
				fset.StringVar(&selector, "selector", "", "node whose centre to move to")
				fset.StringVar(&coordinates, "coordinates", "", "screen coordinates as JSON")
			case "touch":
				fset.StringVar(&position, "position", "", "touch start coordinates as JSON (required)")
				fset.StringVar(&dragTo, "drag-to", "", "drag destination coordinates as JSON")
				fset.StringVar(&touchAction, "action", "", "tap, begin, move or end")
				fset.IntVar(&touchIndex, "index", 0, "finger index for multi-touch")
				fset.IntVar(&durationMs, "duration-ms", 0, "hold duration before release")
			}
		}

		return runWithFlags(ctx, cmd, e, rest, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			params := map[string]any{}
			switch sub {
			case "click":
				setIf(params, "selector", selector, selector != "")
				if position != "" {
					obj, err := parseJSONObject("position", position)
					if err != nil {
						return err
					}
					params["position"] = obj
				}
				setIf(params, "button", button, button != "")
				setIf(params, "double_click", doubleClick, doubleClick)
				return execute(ctx, e, s, "click", params)

			case "key":
				if err := requireArgs(cmd, fset.Args(), 1, "<key>"); err != nil {
					return err
				}
				params["key"] = fset.Arg(0)
				if mods := splitList(modifiers); len(mods) > 0 {
					params["modifiers"] = mods
				}
				setIf(params, "hold_ms", holdMs, wasSet(fset, "hold-ms"))
				return execute(ctx, e, s, "press_key", params)

			case "action":
				if err := requireArgs(cmd, fset.Args(), 1, "<action>"); err != nil {
					return err
				}
				params["action"] = fset.Arg(0)
				setIf(params, "strength", strength, wasSet(fset, "strength"))
				setIf(params, "hold_ms", holdMs, wasSet(fset, "hold-ms"))
				return execute(ctx, e, s, "press_action", params)

			case "text":
				if err := requireArgs(cmd, fset.Args(), 1, "<text>"); err != nil {
					return err
				}
				params["text"] = fset.Arg(0)
				setIf(params, "selector", selector, selector != "")
				setIf(params, "delay_ms", delayMs, wasSet(fset, "delay-ms"))
				return execute(ctx, e, s, "type_text", params)

			case "move":
				setIf(params, "selector", selector, selector != "")
				if coordinates != "" {
					obj, err := parseJSONObject("coordinates", coordinates)
					if err != nil {
						return err
					}
					params["coordinates"] = obj
				}
				return execute(ctx, e, s, "mouse_move", params)

			case "touch":
				if position == "" {
					return usagef(fmt.Errorf("input touch requires --position"))
				}
				obj, err := parseJSONObject("position", position)
				if err != nil {
					return err
				}
				params["position"] = obj
				if dragTo != "" {
					target, err := parseJSONObject("drag-to", dragTo)
					if err != nil {
						return err
					}
					params["drag_to"] = target
				}
				setIf(params, "action", touchAction, touchAction != "")
				setIf(params, "index", touchIndex, wasSet(fset, "index"))
				setIf(params, "duration_ms", durationMs, wasSet(fset, "duration-ms"))
				return execute(ctx, e, s, "touch", params)

			default:
				return usagef(fmt.Errorf("unknown input subcommand %q (want click, key, action, text, move or touch)", sub))
			}
		})
	},
}

// ── wait ──────────────────────────────────────────────────────────────────

var cmdWait = &command{
	name: "wait", usage: "node|signal|property --port N [...]", connects: true,
	summary: "Wait for a node, a signal, or a property condition",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		if len(args) == 0 {
			return usagef(fmt.Errorf("wait needs a subcommand: node, signal or property"))
		}
		sub, rest := args[0], args[1:]

		var state, operator, expected string
		var timeoutMs, pollMs int
		bind := func(fset *flag.FlagSet) {
			fset.IntVar(&timeoutMs, "timeout-ms", 0, "maximum wait in milliseconds")
			switch sub {
			case "node":
				fset.StringVar(&state, "state", "", "exists, visible or removed")
				fset.IntVar(&pollMs, "poll-ms", 0, "poll interval in milliseconds")
			case "property":
				fset.StringVar(&operator, "operator", "", "equals, not_equals, exists, contains, greater_than, less_than")
				fset.StringVar(&expected, "expected", "", "value to compare against (JSON, or a plain string)")
				fset.IntVar(&pollMs, "poll-ms", 0, "poll interval in milliseconds")
			}
		}

		return runWithFlags(ctx, cmd, e, rest, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			params := map[string]any{}
			setIf(params, "timeout_ms", timeoutMs, wasSet(fset, "timeout-ms"))
			setIf(params, "poll_interval_ms", pollMs, wasSet(fset, "poll-ms"))

			switch sub {
			case "node":
				if err := requireArgs(cmd, fset.Args(), 1, "<selector>"); err != nil {
					return err
				}
				params["selector"] = fset.Arg(0)
				setIf(params, "state", state, state != "")
				return execute(ctx, e, s, "wait_for_node", params)

			case "signal":
				if err := requireArgs(cmd, fset.Args(), 2, "<selector> <signal_name>"); err != nil {
					return err
				}
				delete(params, "poll_interval_ms")
				params["selector"] = fset.Arg(0)
				params["signal_name"] = fset.Arg(1)
				return execute(ctx, e, s, "wait_for_signal", params)

			case "property":
				if err := requireArgs(cmd, fset.Args(), 2, "<selector> <property>"); err != nil {
					return err
				}
				if operator == "" {
					return usagef(fmt.Errorf("wait property requires --operator"))
				}
				params["selector"] = fset.Arg(0)
				params["property"] = fset.Arg(1)
				params["operator"] = operator
				if operator != "exists" {
					if !wasSet(fset, "expected") {
						return usagef(fmt.Errorf("operator %q requires --expected", operator))
					}
					params["expected_value"] = parseValue(expected)
				}
				return execute(ctx, e, s, "wait_for_property", params)

			default:
				return usagef(fmt.Errorf("unknown wait subcommand %q (want node, signal or property)", sub))
			}
		})
	},
}

// ── screenshot ────────────────────────────────────────────────────────────

var cmdScreenshot = &command{
	name: "screenshot", usage: "--port N [--out F | --baseline NAME | --diff NAME]", connects: true,
	summary: "Capture the viewport, save a baseline, or diff against one",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var out, nodeSelector, baseline, diff, baselineDir, artifactDir string
		var threshold, sensitivity float64
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&out, "out", "", "write the PNG to this path")
			fset.StringVar(&nodeSelector, "selector", "", "crop to this node's bounding rect")
			fset.StringVar(&baseline, "baseline", "", "save the frame as this named baseline")
			fset.StringVar(&diff, "diff", "", "compare the frame against this named baseline")
			fset.StringVar(&baselineDir, "baseline-dir", "stagehand-baselines", "baseline directory")
			fset.StringVar(&artifactDir, "artifact-dir", "stagehand-diffs", "directory for failed-diff artifacts")
			fset.Float64Var(&threshold, "threshold", 0, "maximum acceptable fraction of differing pixels")
			fset.Float64Var(&sensitivity, "pixel-sensitivity", 0, "per-channel colour tolerance below which a pixel does not count")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, _ *flag.FlagSet) error {
			if baseline != "" && diff != "" {
				return usagef(fmt.Errorf("--baseline and --diff are mutually exclusive: save a baseline or compare against one"))
			}
			shot, err := gwpop.Capture(ctx, s.Caller(), nodeSelector)
			if err != nil {
				return err
			}

			if out != "" {
				if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
					return fmt.Errorf("create output directory: %w", err)
				}
				if err := os.WriteFile(out, shot.PNG, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", out, err)
				}
			}

			switch {
			case baseline != "":
				outcome, err := visual.SaveBaseline(baselineDir, baseline, nodeSelector, shot)
				if err != nil {
					return usagef(err)
				}
				return emit(e.stdout, outcome)

			case diff != "":
				outcome, err := visual.CompareBaseline(visual.DiffConfig{
					BaselineDir: baselineDir, ArtifactDir: artifactDir, Name: diff,
					Selector: nodeSelector, Threshold: threshold, PixelSensitivity: sensitivity,
				}, shot)
				if err != nil {
					return err
				}
				if emitErr := emit(e.stdout, outcome); emitErr != nil {
					return emitErr
				}
				if !outcome.Pass {
					return &assertionError{message: "visual regression against baseline " + diff + "\n" + outcome.Report()}
				}
				return nil

			default:
				result := map[string]any{"width": shot.Width, "height": shot.Height, "mime_type": shot.MimeType}
				if out != "" {
					result["path"] = out
				} else {
					result["data"] = shot.Base64()
				}
				return emit(e.stdout, result)
			}
		})
	},
}

// ── performance ───────────────────────────────────────────────────────────

var cmdPerformance = &command{
	name: "performance", usage: "--port N [--monitors A,B] [--assert MONITOR --threshold N]", connects: true,
	summary: "Read performance monitors, or assert one against a threshold",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		var monitors, assertMonitor, op string
		var threshold float64
		bind := func(fset *flag.FlagSet) {
			fset.StringVar(&monitors, "monitors", "", "comma-separated monitor names, e.g. TIME_FPS,MEMORY_STATIC")
			fset.StringVar(&assertMonitor, "assert", "", "assert this monitor against --threshold")
			fset.Float64Var(&threshold, "threshold", 0, "threshold for --assert")
			fset.StringVar(&op, "op", "", "comparison for --assert: lt, lte, gt, gte, eq (default lte)")
		}
		return runWithFlags(ctx, cmd, e, args, bind, func(ctx context.Context, s *session, fset *flag.FlagSet) error {
			if assertMonitor == "" {
				params := map[string]any{}
				if names := splitList(monitors); len(names) > 0 {
					params["monitors"] = names
				}
				return execute(ctx, e, s, "get_performance", params)
			}
			if !wasSet(fset, "threshold") {
				return usagef(fmt.Errorf("--assert requires --threshold"))
			}
			params := map[string]any{"monitor": assertMonitor, "threshold": threshold}
			setIf(params, "op", op, op != "")

			raw, err := gwpop.Execute(ctx, s.Caller(), gwpop.Op{Action: "assert_performance", Params: params})
			if err != nil {
				return err
			}
			if emitErr := emitRaw(e.stdout, raw); emitErr != nil {
				return emitErr
			}
			outcome, err := gwpop.DecodePerformance(raw)
			if err != nil {
				return err
			}
			if !outcome.Passed {
				return &assertionError{message: fmt.Sprintf("performance monitor %s = %g, want %s %g",
					outcome.Monitor, outcome.Value, outcome.Op, outcome.Threshold)}
			}
			return nil
		})
	},
}

// ── actions (discovery) ───────────────────────────────────────────────────

var cmdActions = &command{
	name: "actions", usage: "", connects: false,
	summary: "List every action a scenario step may use",
	run: func(ctx context.Context, e *env, cmd *command, args []string) error {
		return runWithFlags(ctx, cmd, e, args, nil, func(_ context.Context, _ *session, _ *flag.FlagSet) error {
			var b strings.Builder
			for _, action := range scenario.Actions() {
				spec, _ := scenario.SpecFor(action)
				b.WriteString(fmt.Sprintf("%-20s %s\n", action, spec.Summary))
				if len(spec.Required) > 0 {
					b.WriteString(fmt.Sprintf("%-20s   required: %s\n", "", strings.Join(spec.Required, ", ")))
				}
				optional := append(append([]string{}, spec.Optional...), flatten(spec.OneOf)...)
				if len(optional) > 0 {
					b.WriteString(fmt.Sprintf("%-20s   optional: %s\n", "", strings.Join(dedupe(optional), ", ")))
				}
			}
			_, err := fmt.Fprint(e.stdout, b.String())
			return err
		})
	},
}

func flatten(groups [][]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func rawOrString(raw []byte) any {
	var decoded any
	if err := jsonUnmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	return decoded
}
