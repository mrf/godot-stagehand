package setup

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// DetectWSL reports whether the current process is running under the Windows
// Subsystem for Linux, by inspecting /proc/version for the Microsoft marker.
func DetectWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	v := strings.ToLower(string(data))
	return strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
}

// printGuidance writes the MCP client config snippet and run-your-game guidance
// to out. When opts.IsWSL is true, WSL-specific connection notes are appended.
func printGuidance(out io.Writer, opts Options) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Add this to your MCP client config (e.g. Claude Code / claude_desktop_config.json):")
	fmt.Fprintln(out)
	fmt.Fprint(out, mcpSnippet(opts.BinaryPath))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run your game with:")
	fmt.Fprintf(out, "  godot --path %s --stagehand\n", opts.ProjectPath)

	if opts.IsWSL {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "WSL detected — connection guidance:")
		fmt.Fprintln(out, "  • Linux Godot inside WSL: connect with host 127.0.0.1 (the default).")
		fmt.Fprintln(out, "  • Windows Godot (godot.exe) from WSL: pass a Windows path and pick a host:")
		fmt.Fprintf(out, "      godot.exe --path \"$(wslpath -w %s)\" --stagehand\n", opts.ProjectPath)
		fmt.Fprintln(out, "      - mirrored networking: host localhost")
		fmt.Fprintln(out, "      - default/NAT networking: host = WSL default gateway IP")
		fmt.Fprintln(out, "        (find it with: ip route show default | awk '{print $3}')")
	}
}

// mcpSnippet renders the JSON configuration block that registers this binary as
// the godot-stagehand MCP server.
func mcpSnippet(binaryPath string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "godot-stagehand": {
      "command": %q
    }
  }
}
`, binaryPath)
}
