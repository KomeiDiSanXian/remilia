package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dashboarddist
var dashboardFiles embed.FS

var dashboardFS fs.FS
var dashboardIndex string
var dashboardFileSet map[string]bool

func init() {
	sub, err := fs.Sub(dashboardFiles, "dashboarddist")
	if err != nil {
		return
	}
	dashboardFS = sub
	dashboardIndex = "/index.html"

	// 构建已知文件集合，避免每次请求调 Open
	dashboardFileSet = make(map[string]bool)
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			dashboardFileSet["/"+strings.ReplaceAll(p, "\\", "/")] = true
		}
		return nil
	})
}

// dashboardHandler 返回一个 HTTP handler，在 dashboarddist 存在时 serve SPA。
// 所有未匹配到静态文件的路径均 fallback 到 index.html（支持 History API 路由）。
func dashboardHandler() http.Handler {
	if dashboardFS == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(dashboardFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean(r.URL.Path)
		if dashboardFileSet[p] {
			fileServer.ServeHTTP(w, r)
			return
		}
		// History API fallback：前端路由交给 index.html
		r.URL.Path = dashboardIndex
		fileServer.ServeHTTP(w, r)
	})
}
