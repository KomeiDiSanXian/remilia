//go:build windows

package textimage

// sysCJKFonts returns Windows CJK font candidates in priority order.
//
// Priority: Microsoft YaHei → SimHei → KaiTi → STXiHei → STSong → SimSun → STKaiti
func sysCJKFonts() []string {
	const root = `C:\Windows\Fonts\`
	return []string{
		root + "msyh.ttc",    // Microsoft YaHei  微软雅黑 Regular
		root + "msyhbd.ttc",  // Microsoft YaHei  微软雅黑 Bold
		root + "simhei.ttf",  // SimHei            黑体
		root + "simkai.ttf",  // KaiTi             楷体
		root + "STXIHEI.TTF", // STXiHei           华文细黑
		root + "STSONG.TTF",  // STSong            华文宋体
		root + "simsun.ttc",  // SimSun            宋体
		root + "STKAITI.TTF", // STKaiti           华文楷体
	}
}

// systemFontDirs returns the standard Windows font directories.
func systemFontDirs() []string {
	return []string{`C:\Windows\Fonts`}
}
