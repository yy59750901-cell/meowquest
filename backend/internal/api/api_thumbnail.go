package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const (
	thumbnailEdge          = 480
	thumbnailMaxPixels     = 20_000_000
	thumbnailMaxSide       = 16384
	thumbnailMaxBytes      = 20 * 1024 * 1024
	thumbnailMaxCacheBytes = 1024 * 1024
	thumbnailSuffix        = ".thumb.jpg"
	thumbnailCacheHeader   = "public, max-age=31536000, immutable"
)

var (
	errThumbnailNotFound    = errors.New("图片不存在或路径不安全")
	errThumbnailUnsupported = errors.New("不支持此图片格式（HEIC/WebP 暂不支持），请回退原图或转换为 JPEG/PNG")
	errThumbnailInvalid     = errors.New("图片损坏或无法解码，请回退原图")
	errThumbnailTooLarge    = errors.New("图片超过预览大小上限，请回退原图")
	// 上传和存量请求共用解码配额，不让展开列表触发多个大图同时解码。
	thumbnailSlots     = make(chan struct{}, 1)
	evidenceThumbnails = thumbnailCache{flights: make(map[string]*thumbnailFlight)}
)

type thumbnailFlight struct {
	done chan struct{}
	err  error
}

type thumbnailCache struct {
	mu      sync.Mutex
	flights map[string]*thumbnailFlight
}

func thumbnailURL(mediaPath string) string {
	return "/api/media/thumbnail?path=" + url.QueryEscape(mediaPath)
}

// GET /api/media/thumbnail?path=/uploads/…，沿用公开 uploads 的授权语义。
func (s *Server) mediaThumbnail(c *gin.Context) {
	serveMediaThumbnail(c, uploadDir())
}

func serveMediaThumbnail(c *gin.Context, dir string) {
	values := c.Request.URL.Query()["path"]
	if len(values) != 1 {
		writeThumbnailError(c, errThumbnailNotFound)
		return
	}
	f, err := evidenceThumbnails.open(c.Request.Context(), dir, values[0])
	if err != nil {
		writeThumbnailError(c, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeThumbnailError(c, err)
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", thumbnailCacheHeader)
	http.ServeContent(c.Writer, c.Request, "preview.jpg", info.ModTime(), f)
}

func thumbnailErrorResponse(err error) (int, string, string) {
	switch {
	case errors.Is(err, errThumbnailNotFound):
		return http.StatusNotFound, "thumbnail_not_found", errThumbnailNotFound.Error()
	case errors.Is(err, errThumbnailUnsupported):
		return http.StatusUnsupportedMediaType, "thumbnail_unsupported", errThumbnailUnsupported.Error()
	case errors.Is(err, errThumbnailInvalid):
		return http.StatusUnsupportedMediaType, "thumbnail_invalid_image", errThumbnailInvalid.Error()
	case errors.Is(err, errThumbnailTooLarge):
		return http.StatusUnsupportedMediaType, "thumbnail_too_large", errThumbnailTooLarge.Error()
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, "thumbnail_timeout", "预览请求已取消或超时，请重试"
	default:
		return http.StatusInternalServerError, "thumbnail_failed", "预览生成失败，请回退原图或稍后重试"
	}
}

func writeThumbnailError(c *gin.Context, err error) {
	status, code, message := thumbnailErrorResponse(err)
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": message, "code": code})
}

func thumbnailRelativePath(mediaPath string) (string, error) {
	if !strings.HasPrefix(mediaPath, "/uploads/") || strings.ContainsAny(mediaPath, "\\%?#\x00:") {
		return "", errThumbnailNotFound
	}
	rel := strings.TrimPrefix(mediaPath, "/uploads/")
	if !filepath.IsLocal(rel) || path.Clean(rel) != rel {
		return "", errThumbnailNotFound
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", errThumbnailNotFound
		}
	}
	if strings.HasSuffix(strings.ToLower(rel), thumbnailSuffix) {
		return "", errThumbnailNotFound
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".gif":
		return rel, nil
	default:
		return "", errThumbnailUnsupported
	}
}

// 所有读取、临时文件和重命名均基于受限根目录；固定父目录句柄避免检查后被换成链接。
func thumbnailDirectory(dir, rel string) (*os.Root, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errThumbnailNotFound
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errThumbnailNotFound
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errThumbnailNotFound
	}
	parent := path.Dir(rel)
	parentInfo := opened
	if parent != "." {
		prefix := ""
		for _, part := range strings.Split(parent, "/") {
			prefix = path.Join(prefix, part)
			info, err := root.Lstat(prefix)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, errThumbnailNotFound
			}
			parentInfo = info
		}
	}
	child, err := root.OpenRoot(parent)
	if err != nil {
		return nil, errThumbnailNotFound
	}
	opened, err = child.Stat(".")
	if err != nil || !os.SameFile(parentInfo, opened) {
		child.Close()
		return nil, errThumbnailNotFound
	}
	return child, nil
}

func thumbnailOpenRegular(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errThumbnailNotFound
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, errThumbnailNotFound
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, errThumbnailNotFound
	}
	return f, nil
}

func openThumbnailCache(root *os.Root, name string) (*os.File, error) {
	f, err := thumbnailOpenRegular(root, name+thumbnailSuffix)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() < 1 || info.Size() > thumbnailMaxCacheBytes {
		f.Close()
		return nil, errThumbnailInvalid
	}
	cfg, err := jpeg.DecodeConfig(io.LimitReader(f, thumbnailMaxCacheBytes))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > thumbnailEdge || cfg.Height > thumbnailEdge {
		f.Close()
		return nil, errThumbnailInvalid
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (cache *thumbnailCache) open(ctx context.Context, dir, mediaPath string) (*os.File, error) {
	rel, err := thumbnailRelativePath(mediaPath)
	if err != nil {
		return nil, err
	}
	root, err := thumbnailDirectory(dir, rel)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := path.Base(rel)
	source, err := thumbnailOpenRegular(root, name)
	if err != nil {
		return nil, errThumbnailNotFound
	}
	source.Close()
	f, err := openThumbnailCache(root, name)
	if err == nil || !os.IsNotExist(err) {
		return f, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	err = cache.do(ctx, filepath.Join(absDir, filepath.FromSlash(rel)), func() error {
		return generateThumbnail(root, name)
	})
	if err != nil {
		return nil, err
	}
	return openThumbnailCache(root, name)
}

func (cache *thumbnailCache) do(ctx context.Context, key string, generate func() error) error {
	cache.mu.Lock()
	if call := cache.flights[key]; call != nil {
		cache.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
			return call.err
		}
	}
	call := &thumbnailFlight{done: make(chan struct{})}
	cache.flights[key] = call
	cache.mu.Unlock()

	select {
	case <-ctx.Done():
		call.err = ctx.Err()
	case thumbnailSlots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			call.err = err
		} else {
			call.err = generate()
		}
		<-thumbnailSlots
	}
	cache.mu.Lock()
	delete(cache.flights, key)
	close(call.done)
	cache.mu.Unlock()
	return call.err
}

func generateThumbnail(root *os.Root, name string) error {
	// 请求可能在首个生成完成后才取得锁，此处再次检查，避免重复解码和重写。
	if f, err := openThumbnailCache(root, name); err == nil {
		f.Close()
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	source, err := thumbnailOpenRegular(root, name)
	if err != nil {
		return errThumbnailNotFound
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if info.Size() > thumbnailMaxBytes {
		return errThumbnailTooLarge
	}
	// 配置检查和解码使用同一份有界快照，防止两次读取之间内容变化绕过像素限制。
	data, err := io.ReadAll(io.LimitReader(source, thumbnailMaxBytes+1))
	if err != nil {
		return err
	}
	if len(data) > thumbnailMaxBytes {
		return errThumbnailTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif") {
		return errThumbnailInvalid
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > thumbnailMaxSide || cfg.Height > thumbnailMaxSide || cfg.Width > thumbnailMaxPixels/cfg.Height {
		return errThumbnailTooLarge
	}
	original, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return errThumbnailInvalid
	}
	// GIF首帧可以小于逻辑画布，实际帧仍须位于已校验的尺寸上限内。
	bounds := original.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 || bounds.Dx() > cfg.Width || bounds.Dy() > cfg.Height {
		return errThumbnailInvalid
	}
	preview := resizeThumbnail(original)
	tmpName := "." + name + ".thumb-" + rand.Text() + ".tmp"
	tmp, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer root.Remove(tmpName)
	if err := jpeg.Encode(tmp, preview, &jpeg.Options{Quality: 82}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return root.Rename(tmpName, name+thumbnailSuffix)
}

// 整数盒式采样保留比例、不放大小图，透明像素合成到白底；只额外分配预览大小的内存。
func resizeThumbnail(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	w, h := sw, sh
	if sw > thumbnailEdge || sh > thumbnailEdge {
		if sw >= sh {
			w, h = thumbnailEdge, max(1, (sh*thumbnailEdge+sw/2)/sw)
		} else {
			w, h = max(1, (sw*thumbnailEdge+sh/2)/sh), thumbnailEdge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			var rr, gg, bb uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					rr += uint64(r + 0xffff - a)
					gg += uint64(g + 0xffff - a)
					bb += uint64(b + 0xffff - a)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(rr/n>>8), uint8(gg/n>>8), uint8(bb/n>>8), 255
		}
	}
	return dst
}
