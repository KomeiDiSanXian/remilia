//go:build !windows && !darwin

package textimage

import "os"

// sysCJKFonts returns Linux/Unix CJK font candidates in priority order.
//
// Searches common font base directories for well-known CJK font files:
// Noto Sans CJK SC → WenQuanYi Micro Hei → AR PL UMing/UKai →
// Google Noto → Source Han Sans
func sysCJKFonts() []string {
	bases := []string{
		"/usr/share/fonts",
		"/usr/local/share/fonts",
		os.Getenv("HOME") + "/.local/share/fonts",
	}
	suffixes := []string{
		"/noto/NotoSansCJK-Regular.ttc",
		"/noto-cjk/NotoSansCJKsc-Regular.otf",
		"/opentype/noto/NotoSansCJK-Regular.ttc",
		"/truetype/wqy/wqy-microhei.ttc",
		"/wqy-microhei/wqy-microhei.ttc",
		"/truetype/arphic/uming.ttc",
		"/truetype/arphic/ukai.ttc",
		"/google-noto/NotoSansCJKsc-Regular.otf",
		"/source-han-sans/SourceHanSansSC-Regular.otf",
	}
	paths := make([]string, 0, len(bases)*len(suffixes))
	for _, base := range bases {
		for _, sfx := range suffixes {
			paths = append(paths, base+sfx)
		}
	}
	return paths
}

// systemFontDirs returns the standard Linux/Unix font directories.
func systemFontDirs() []string {
	return []string{
		"/usr/share/fonts",
		"/usr/local/share/fonts",
		os.Getenv("HOME") + "/.local/share/fonts",
	}
}
