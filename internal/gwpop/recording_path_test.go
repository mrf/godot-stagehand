package gwpop

import (
	"strings"
	"testing"
)

func TestResolveRecordingPathKeepsPathsInsideTheRecordingDir(t *testing.T) {
	cases := map[string]string{
		"ok.json":                              "user://stagehand_recordings/ok.json",
		"runs/menu_flow-2.json":                "user://stagehand_recordings/runs/menu_flow-2.json",
		"user://stagehand_recordings/ok.json":  "user://stagehand_recordings/ok.json",
		"user://stagehand_recordings/a/b.json": "user://stagehand_recordings/a/b.json",
		"rec-1730000000000-1.json":             "user://stagehand_recordings/rec-1730000000000-1.json",
		"user://stagehand_recordings/x.y.json": "user://stagehand_recordings/x.y.json",
	}
	for in, want := range cases {
		got, err := ResolveRecordingPath(in)
		if err != nil {
			t.Errorf("ResolveRecordingPath(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ResolveRecordingPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveRecordingPathRejectsEscapes(t *testing.T) {
	for _, in := range []string{
		"",
		"res://x.json",
		"res://player.gd",
		"user://x.json",
		"user://../x.json",
		"user://stagehand_recordings",
		"user://stagehand_recordings/",
		"user://stagehand_recordings/../x.json",
		"user://stagehand_recordings_evil/x.json",
		"/tmp/x.json",
		"/home/user/.bashrc",
		"C:/Users/x.json",
		`C:\Users\x.json`,
		`sub\x.json`,
		"../x.json",
		"a/../../x.json",
		"./x.json",
		"a//x.json",
		"x.json/",
		"file:///tmp/x.json",
		"x y.json",
		"x\x00.json",
	} {
		if got, err := ResolveRecordingPath(in); err == nil {
			t.Errorf("ResolveRecordingPath(%q) = %q, want an error", in, got)
		}
	}
}

func TestResolveRecordingPathErrorNamesTheRule(t *testing.T) {
	_, err := ResolveRecordingPath("res://player.gd")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{`"res://player.gd"`, RecordingDir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRecordOpsRejectEscapingPathsBeforeSending(t *testing.T) {
	for _, op := range []Op{
		{Action: "record_start", Params: map[string]any{"output_path": "res://overwrite_me.gd"}},
		{Action: "replay", Params: map[string]any{"input_path": "/etc/passwd"}},
	} {
		spec, _ := Lookup(op.Action)
		_, err := spec.Params(op.Params)
		if err == nil {
			t.Errorf("%s accepted %v", op.Action, op.Params)
			continue
		}
		if KindOf(err) != KindUsage {
			t.Errorf("%s: kind = %v, want usage", op.Action, KindOf(err))
		}
	}
}

func TestRecordOpsSendTheResolvedPath(t *testing.T) {
	spec, _ := Lookup("record_start")
	params, err := spec.Params(map[string]any{"output_path": "run1.json"})
	if err != nil {
		t.Fatal(err)
	}
	if got := params["output_path"]; got != "user://stagehand_recordings/run1.json" {
		t.Errorf("output_path on the wire = %v", got)
	}
}
