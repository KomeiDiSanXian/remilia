//go:build !windows

package terminal

// enableVT100 在非 Windows 平台上为空操作（VT100 默认支持）。
func enableVT100(_ uintptr) {}
