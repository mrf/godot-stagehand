package gwpop

import (
	"fmt"
	"strings"
)

// RecordingDir is the only directory record_start writes to and replay reads
// from. The addon applies the identical rule (input_recorder.gd
// resolve_recording_path) and relaxes it only under STAGEHAND_ALLOW_UNSAFE=1;
// the Go clients always apply it. See docs/security.md.
const RecordingDir = "user://stagehand_recordings"

// RecordingPathSyntax describes an accepted recording path, for error messages.
const RecordingPathSyntax = "a relative path such as \"run1.json\" or \"runs/run1.json\", or one starting with " +
	RecordingDir + "/; each segment uses letters, digits, '_', '-' and '.', and '.'/'..' segments are refused"

// ResolveRecordingPath maps a caller-supplied recording path onto the file it
// names inside RecordingDir, or returns an error when it would escape it.
// A path already under RecordingDir is kept; any other scheme (res://, a bare
// user://), an absolute path, a drive letter, a backslash, and a '.' or '..'
// segment are refused rather than normalised, so a refusal never depends on
// how a platform resolves the result.
func ResolveRecordingPath(path string) (string, error) {
	reject := func(reason string) (string, error) {
		return "", fmt.Errorf("invalid recording path %q: %s (allowed: %s)", path, reason, RecordingPathSyntax)
	}
	rel := path
	if rest, ok := strings.CutPrefix(path, RecordingDir+"/"); ok {
		rel = rest
	} else if strings.Contains(path, ":") {
		return reject("only paths under " + RecordingDir + "/ may name a scheme or drive")
	} else if strings.HasPrefix(path, "/") {
		return reject("absolute paths are not allowed")
	}
	if rel == "" {
		return reject("a file name is required")
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return reject(fmt.Sprintf("segment %q is not allowed", segment))
		}
		for _, r := range segment {
			if !isRecordingPathRune(r) {
				return reject(fmt.Sprintf("character %q is not allowed", r))
			}
		}
	}
	return RecordingDir + "/" + rel, nil
}

func isRecordingPathRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '_' || r == '-' || r == '.'
}
