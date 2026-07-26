package launch

import "strings"

// A Windows Godot binary launched from WSL requires project_path in UNC form
// (\\wsl.localhost\<Distro>\... or the forward-slash equivalent), because a
// plain Linux path like /home/user/project means nothing to the Windows
// build. But this Go process still runs on the WSL side and needs a real
// local filesystem path for its own operations — the import-stamp directory
// and the cross-process import lock — so a UNC path handed to os.MkdirAll
// verbatim resolves to a bogus location at the WSL filesystem root (e.g.
// "mkdir /wsl.localhost: permission denied"). wslUNCToLocalPath translates
// that UNC form back to the local path it refers to, when the UNC host is
// wsl.localhost (or its legacy wsl$ alias) — the only host that maps back
// onto this process's own filesystem.

// wslUNCToLocalPath translates a \\wsl.localhost\<Distro>\<rest> (or
// //wsl.localhost/<Distro>/<rest>) UNC path into the absolute Linux path
// /<rest> it refers to. ok is false for any path this cannot translate: a
// plain non-UNC path, or a UNC path whose host isn't wsl.localhost/wsl$ (we
// have no way to map an arbitrary UNC share back onto a local path).
func wslUNCToLocalPath(p string) (local string, ok bool) {
	normalized := strings.ReplaceAll(p, `\`, "/")
	rest, isUNC := strings.CutPrefix(normalized, "//")
	if !isUNC {
		return "", false
	}
	segments := strings.SplitN(rest, "/", 3)
	if len(segments) < 3 {
		return "", false
	}
	host, _, remainder := segments[0], segments[1], segments[2]
	if !strings.EqualFold(host, "wsl.localhost") && !strings.EqualFold(host, "wsl$") {
		return "", false
	}
	return "/" + remainder, true
}

// localProjectPath returns the path this process should use for its own
// filesystem operations (import stamp, import lock) on a project passed as
// project_path. A translatable WSL UNC path yields the local path it maps to;
// any other form (a plain path, or a UNC form we cannot translate) is
// returned unchanged.
func localProjectPath(projectPath string) string {
	if local, ok := wslUNCToLocalPath(projectPath); ok {
		return local
	}
	return projectPath
}
