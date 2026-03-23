package textimage

import "os"

// SystemCJKFontPath returns the absolute path to a suitable CJK (Chinese /
// Japanese / Korean) font installed on the current operating system, or an
// empty string if none is found.
//
// Platform-specific search priorities are defined in the sibling sysfont_*.go
// files via conditional compilation:
//
//   - Windows  → sysfont_windows.go
//   - macOS    → sysfont_darwin.go
//   - others   → sysfont_unix.go
func SystemCJKFontPath() string {
	return firstExisting(sysCJKFonts()...)
}

// SystemFontPath looks up a font by (case-insensitive) filename in the
// platform's standard font directory.  Returns "" when not found.
func SystemFontPath(filename string) string {
	for _, dir := range systemFontDirs() {
		p := dir + string(os.PathSeparator) + filename
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
