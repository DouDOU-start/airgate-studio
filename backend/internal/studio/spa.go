package studio

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// webdist 前端构建产物，由 Makefile 在构建前从 web/dist 同步（见 sync-webdist）。
//
//go:embed all:webdist
var webDistFS embed.FS

// spaHandler 托管 SPA 前端：命中静态文件直接返回，未命中回退 index.html（history 路由）。
//
// 开发模式下若 web/dist 在磁盘上可用（go run 场景），优先读磁盘，免去每次改前端都要重编后端。
func (s *Server) spaHandler() http.Handler {
	var dist fs.FS
	if dir := devDistDir(); dir != "" {
		dist = os.DirFS(dir)
	} else {
		sub, err := fs.Sub(webDistFS, "webdist")
		if err != nil {
			panic("webdist 嵌入目录缺失: " + err.Error())
		}
		dist = sub
	}

	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback：一律回 index.html，交给前端路由。
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "前端资产未构建（先执行 make build 或 cd web && pnpm build）", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

// devDistDir 尝试定位磁盘上的 web/dist（含 index.html 才算有效）。
func devDistDir() string {
	for _, dir := range []string{
		filepath.Join("..", "web", "dist"),
		filepath.Join("web", "dist"),
	} {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return dir
		}
	}
	return ""
}
