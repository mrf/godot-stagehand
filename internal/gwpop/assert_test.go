package gwpop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mrf/godot-stagehand/internal/visual/visualtest"
)

func TestCaptureSendsFullPageBySelector(t *testing.T) {
	cases := []struct {
		name         string
		nodeSelector string
		wantFullPage bool
		wantSelector bool
	}{
		{name: "no selector captures the full viewport", nodeSelector: "", wantFullPage: true, wantSelector: false},
		{name: "a selector crops instead of capturing full page", nodeSelector: "#Player", wantFullPage: false, wantSelector: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := json.Marshal(map[string]any{
				"data":      visualtest.SolidPNGBase64(4, 4, visualtest.Opaque),
				"mime_type": "image/png",
			})
			if err != nil {
				t.Fatalf("marshal fake screenshot result: %v", err)
			}
			caller := &recordingCaller{result: result}

			if _, err := Capture(context.Background(), caller, tc.nodeSelector); err != nil {
				t.Fatalf("Capture() error = %v", err)
			}

			params := paramsMap(t, caller.params)
			fullPage, ok := params["full_page"].(bool)
			if !ok {
				t.Fatalf("params[full_page] = %v (%T), want bool", params["full_page"], params["full_page"])
			}
			if fullPage != tc.wantFullPage {
				t.Errorf("full_page = %v, want %v", fullPage, tc.wantFullPage)
			}

			_, hasSelector := params["selector"]
			if hasSelector != tc.wantSelector {
				t.Errorf("selector present = %v, want %v", hasSelector, tc.wantSelector)
			}
			if tc.wantSelector && params["selector"] != tc.nodeSelector {
				t.Errorf("params[selector] = %v, want %v", params["selector"], tc.nodeSelector)
			}
		})
	}
}
