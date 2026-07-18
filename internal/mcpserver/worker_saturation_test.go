package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	mcpserverlib "github.com/mark3labs/mcp-go/server"
)

func TestStdioWorkersRecoverAndLifecycleToolsRespondAfterSilentCalls(t *testing.T) {
	host, port, _ := deadlineStubGodot(t)
	srv := New()
	srv.callTimeout = time.Second
	t.Cleanup(srv.clearConn)
	connectDeadlineStub(t, srv, host, port)

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	stdio := mcpserverlib.NewStdioServer(srv.mcp)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- stdio.Listen(ctx, stdinReader, stdoutWriter)
		_ = stdoutWriter.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_ = stdinWriter.Close()
		_ = stdoutReader.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("stdio server did not stop")
		}
	})

	responses := make(chan map[string]any, 16)
	go scanStdioResponses(stdoutReader, responses)
	writeStdioRequest(t, stdinWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]any{
				"name":    "liveness-test",
				"version": "1.0.0",
			},
		},
	})
	waitForStdioResponse(t, responses, 1, time.Second)

	// The stdio transport has five workers. Four silent Godot calls may occupy
	// remote-call slots, while the fifth must be rejected quickly so one worker
	// remains available for local status and disconnect requests.
	for id := 10; id < 15; id++ {
		writeStdioToolCall(t, stdinWriter, id, "godot_get_tree", map[string]any{})
	}
	overload := waitForAnyStdioResponse(t, responses, 250*time.Millisecond, 10, 11, 12, 13, 14)
	if text := stdioResultText(t, overload); !strings.Contains(text, "in-flight Godot call limit") {
		t.Fatalf("fifth call was not rejected as overload: %s", text)
	}
	started := time.Now()
	writeStdioToolCall(t, stdinWriter, 20, "godot_status", map[string]any{})
	writeStdioToolCall(t, stdinWriter, 21, "godot_disconnect", map[string]any{"instance_id": "default"})

	lifecycle := waitForStdioResponses(t, responses, time.Second, 20, 21)
	status := lifecycle[20]
	disconnect := lifecycle[21]
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("lifecycle tools remained blocked for %s after silent calls", elapsed)
	}
	if text := stdioResultText(t, status); text == "" {
		t.Fatal("godot_status returned empty text")
	}
	if text := stdioResultText(t, disconnect); text == "" {
		t.Fatal("godot_disconnect returned empty text")
	}
}

func waitForAnyStdioResponse(
	t *testing.T,
	responses <-chan map[string]any,
	timeout time.Duration,
	wantIDs ...int,
) map[string]any {
	t.Helper()
	wanted := make(map[int]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		wanted[id] = struct{}{}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case response, ok := <-responses:
			if !ok {
				t.Fatal("stdio closed before overload response")
			}
			id, ok := response["id"].(float64)
			if !ok {
				continue
			}
			if _, match := wanted[int(id)]; match {
				return response
			}
		case <-timer.C:
			t.Fatalf("no call was rejected before worker saturation; ids=%v", wantIDs)
		}
	}
}

func scanStdioResponses(reader io.Reader, responses chan<- map[string]any) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var response map[string]any
		if json.Unmarshal(scanner.Bytes(), &response) == nil {
			responses <- response
		}
	}
	close(responses)
}

func writeStdioToolCall(t *testing.T, writer io.Writer, id int, name string, arguments map[string]any) {
	t.Helper()
	writeStdioRequest(t, writer, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	})
}

func writeStdioRequest(t *testing.T, writer io.Writer, request map[string]any) {
	t.Helper()
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal stdio request: %v", err)
	}
	payload = append(payload, '\n')
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("write stdio request: %v", err)
	}
}

func waitForStdioResponse(
	t *testing.T,
	responses <-chan map[string]any,
	wantID int,
	timeout time.Duration,
) map[string]any {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case response, ok := <-responses:
			if !ok {
				t.Fatalf("stdio closed before response %d", wantID)
			}
			if id, ok := response["id"].(float64); ok && int(id) == wantID {
				return response
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for stdio response %d", wantID)
		}
	}
}

func waitForStdioResponses(
	t *testing.T,
	responses <-chan map[string]any,
	timeout time.Duration,
	wantIDs ...int,
) map[int]map[string]any {
	t.Helper()
	wanted := make(map[int]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		wanted[id] = struct{}{}
	}
	found := make(map[int]map[string]any, len(wantIDs))
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(found) < len(wanted) {
		select {
		case response, ok := <-responses:
			if !ok {
				t.Fatalf("stdio closed after %d of %d expected responses", len(found), len(wanted))
			}
			id, ok := response["id"].(float64)
			if !ok {
				continue
			}
			intID := int(id)
			if _, keep := wanted[intID]; keep {
				found[intID] = response
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for stdio responses %v", wantIDs)
		}
	}
	return found
}

func stdioResultText(t *testing.T, response map[string]any) string {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("stdio response has no result: %#v", response)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("stdio response has no content: %#v", response)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("stdio response content has type %T", content[0])
	}
	text, _ := first["text"].(string)
	return text
}
