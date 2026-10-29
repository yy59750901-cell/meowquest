package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// FS 前端产物根（单二进制部署）
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler 静态伺服 + SPA fallback
// HTML 禁缓存（保证发版即生效）；带 hash 的资源长缓存
func Handler() (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		// 资源存在直接伺服；否则回 index.html（hash 路由兜底）
		isFallback := false
		if _, err := fs.Stat(sub, p[1:]); err != nil {
			r.URL.Path = "/"
			isFallback = true
		}
		if p == "/index.html" || p == "/" || isFallback || p == "/manifest.json" {
			w.Header().Set("Cache-Control", "no-cache") // html 每次校验，防浏览器缓存旧版本引用
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
