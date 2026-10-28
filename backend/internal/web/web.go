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
		if _, err := fs.Stat(sub, p[1:]); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
