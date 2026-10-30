package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func thumbnailTestCache() *thumbnailCache {
	return &thumbnailCache{flights: make(map[string]*thumbnailFlight)}
}

func thumbnailTestImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 50, 120, 200, 255
	}
	var b bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&b, img, nil)
	case "png":
		err = png.Encode(&b, img)
	case "gif":
		err = gif.Encode(&b, img, nil)
	default:
		t.Fatalf("未知测试格式 %s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func thumbnailTestWrite(t *testing.T, dir, rel string, data []byte) string {
	t.Helper()
	name := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestThumbnailDimensionsAndOriginalUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, format, ext  string
		w, h, wantW, wantH int
	}{
		{"横向照片", "jpeg", ".jpg", 2048, 1536, 480, 360},
		{"纵向照片", "jpeg", ".JPEG", 1536, 2048, 360, 480},
		{"PNG方图", "png", ".png", 600, 600, 480, 480},
		{"GIF首帧", "gif", ".gif", 640, 320, 480, 240},
		{"小图不放大", "png", ".png", 32, 16, 32, 16},
		{"极窄图片", "jpeg", ".jpg", 1, 1000, 1, 480},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			rel := "202610/original" + tc.ext
			original := thumbnailTestImage(t, tc.format, tc.w, tc.h)
			name := thumbnailTestWrite(t, dir, rel, original)
			before, _ := os.Stat(name)
			cache := thumbnailTestCache()
			f, err := cache.open(context.Background(), dir, "/uploads/"+rel)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := jpeg.Decode(f)
			cacheBefore, _ := f.Stat()
			f.Close()
			if err != nil || preview.Bounds().Dx() != tc.wantW || preview.Bounds().Dy() != tc.wantH {
				t.Fatalf("尺寸错误：图片 %v，错误 %v", preview, err)
			}
			// 新实例也能使用磁盘缓存，且不重写文件。
			f, err = thumbnailTestCache().open(context.Background(), dir, "/uploads/"+rel)
			if err != nil {
				t.Fatal(err)
			}
			cacheAfter, _ := f.Stat()
			f.Close()
			if !os.SameFile(cacheBefore, cacheAfter) || !cacheBefore.ModTime().Equal(cacheAfter.ModTime()) {
				t.Fatal("缓存命中发生了重写")
			}
			after, _ := os.Stat(name)
			got, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(got, original) || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("原图被修改")
			}
			entries, _ := os.ReadDir(filepath.Dir(name))
			if len(entries) != 2 {
				t.Fatalf("应仅有原图和缓存，实际 %d 个文件", len(entries))
			}
		})
	}
}

func TestThumbnailGIFPartialFirstFrame(t *testing.T) {
	frame := image.NewPaletted(image.Rect(3, 5, 23, 15), color.Palette{color.White, color.Black})
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{
		Image: []*image.Paletted{frame}, Delay: []int{0},
		Config: image.Config{ColorModel: frame.Palette, Width: 40, Height: 30},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	thumbnailTestWrite(t, dir, "a.gif", data.Bytes())
	f, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/a.gif")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatalf("GIF局部首帧预览错误：%+v %v", cfg, err)
	}
}

func TestThumbnailTransparentBackgroundAndOffset(t *testing.T) {
	img := image.NewNRGBA(image.Rect(10, 20, 12, 21))
	img.SetNRGBA(11, 20, color.NRGBA{R: 255, A: 128})
	preview := resizeThumbnail(img)
	if got := preview.RGBAAt(0, 0); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("透明像素不是白底：%v", got)
	}
	if got := preview.RGBAAt(1, 0); got.R != 255 || got.G < 126 || got.G > 128 || got.B != got.G || got.A != 255 {
		t.Fatalf("半透明像素合成错误：%v", got)
	}
}

func TestThumbnailRejectsUnsafeAndUnsupportedPaths(t *testing.T) {
	for _, mediaPath := range []string{
		"", "/uploads/", "/etc/passwd", "/uploads-other/a.jpg", "uploads/a.jpg", "/uploads//a.jpg",
		"/uploads/../a.jpg", "/uploads/a/../../a.jpg", "/uploads/a/./b.jpg", "/uploads/a/../b.jpg",
		"/uploads/%2e%2e/a.jpg", "/uploads/%252e%252e/a.jpg", "/uploads/a%2fb.jpg",
		"/uploads/a\\b.jpg", "/uploads/a.jpg\x00", "/uploads/a.jpg?x=1", "/uploads/a.jpg#x",
		"/uploads/C:/a.jpg", "/uploads/.hidden/a.jpg", "/uploads/a.jpg.thumb.jpg", "/uploads/a.JPG.THUMB.JPG",
	} {
		t.Run(mediaPath, func(t *testing.T) {
			_, err := thumbnailRelativePath(mediaPath)
			if !errors.Is(err, errThumbnailNotFound) {
				t.Fatalf("危险路径未拒绝：%q，%v", mediaPath, err)
			}
		})
	}
	for _, ext := range []string{".heic", ".webp", ".svg", ".mp4", ".txt", ".avif"} {
		_, err := thumbnailRelativePath("/uploads/a" + ext)
		if !errors.Is(err, errThumbnailUnsupported) {
			t.Fatalf("格式 %s 未返回不支持：%v", ext, err)
		}
	}
}

func TestThumbnailRejectsSymlinksAndMissingFiles(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	data := thumbnailTestImage(t, "jpeg", 10, 10)
	thumbnailTestWrite(t, dir, "real/a.jpg", data)
	outsideFile := thumbnailTestWrite(t, outside, "a.jpg", data)
	links := map[string]string{
		"outside.jpg":          outsideFile,
		"inside.jpg":           "real/a.jpg",
		"external":             outside,
		"internal":             "real",
		"real/a.jpg.thumb.jpg": outsideFile,
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"outside.jpg", "inside.jpg", "external/a.jpg", "internal/a.jpg", "real/a.jpg", "absent.jpg", "real"} {
		_, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/"+rel)
		status, _, _ := thumbnailErrorResponse(err)
		want := http.StatusNotFound
		if rel == "real" {
			want = http.StatusUnsupportedMediaType
		}
		if status != want {
			t.Fatalf("路径 %s 返回 %d，错误 %v", rel, status, err)
		}
	}
	if err := os.Symlink(dir, filepath.Join(outside, "uploads-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnailTestCache().open(context.Background(), filepath.Join(outside, "uploads-link"), "/uploads/real/a.jpg"); !errors.Is(err, errThumbnailNotFound) {
		t.Fatalf("uploads根目录链接未拒绝：%v", err)
	}
	got, _ := os.ReadFile(outsideFile)
	if !bytes.Equal(got, data) {
		t.Fatal("外部链接目标被修改")
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.jpg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/directory.jpg"); !errors.Is(err, errThumbnailNotFound) {
		t.Fatalf("目录被当作图片：%v", err)
	}
}

func TestThumbnailCorruptAndOversizedImages(t *testing.T) {
	header := thumbnailTestImage(t, "jpeg", 1, 1)
	marker := bytes.Index(header, []byte{0xff, 0xc0})
	if marker < 0 {
		t.Fatal("测试JPEG缺少尺寸标记")
	}
	oversized := append([]byte(nil), header...)
	binary.BigEndian.PutUint16(oversized[marker+5:marker+7], 5000)
	binary.BigEndian.PutUint16(oversized[marker+7:marker+9], 5000)
	tooWide := append([]byte(nil), header...)
	binary.BigEndian.PutUint16(tooWide[marker+7:marker+9], thumbnailMaxSide+1)
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"垃圾数据", []byte("not an image"), errThumbnailInvalid},
		{"仅有文件头", header[:marker+12], errThumbnailInvalid},
		{"尾部损坏", header[:len(header)-20], errThumbnailInvalid},
		{"像素超限", oversized, errThumbnailTooLarge},
		{"单边超限", tooWide, errThumbnailTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			name := thumbnailTestWrite(t, dir, "a.jpg", tc.data)
			_, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/a.jpg")
			if !errors.Is(err, tc.want) {
				t.Fatalf("期望 %v，实际 %v", tc.want, err)
			}
			if _, err := os.Stat(name + thumbnailSuffix); !os.IsNotExist(err) {
				t.Fatal("失败产生了缓存文件")
			}
			got, _ := os.ReadFile(name)
			if !bytes.Equal(got, tc.data) {
				t.Fatal("坏图原文件被修改")
			}
		})
	}
	dir := t.TempDir()
	name := thumbnailTestWrite(t, dir, "large.jpg", header)
	if err := os.Truncate(name, thumbnailMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/large.jpg"); !errors.Is(err, errThumbnailTooLarge) {
		t.Fatalf("文件大小上限未生效：%v", err)
	}
}

// Done被调用时请求已进入single-flight选择分支，以通道同步避免依赖调度延时。
type thumbnailObservedContext struct {
	context.Context
	once  sync.Once
	ready chan struct{}
}

func (c *thumbnailObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.ready) })
	return c.Context.Done()
}

func thumbnailWait(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("等待并发测试超时")
	}
}

func TestThumbnailSingleFlightAndGlobalLimit(t *testing.T) {
	cache := thumbnailTestCache()
	thumbnailSlots <- struct{}{}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { <-thumbnailSlots }) }
	t.Cleanup(release)
	var generated atomic.Int32
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		ctx := &thumbnailObservedContext{Context: context.Background(), ready: make(chan struct{})}
		go func() {
			results <- cache.do(ctx, "same-original", func() error {
				generated.Add(1)
				return nil
			})
		}()
		thumbnailWait(t, ctx.ready)
	}
	release()
	for i := 0; i < 20; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if generated.Load() != 1 || len(cache.flights) != 0 {
		t.Fatalf("未去重或未释放锁：生成 %d 次", generated.Load())
	}
	var active, peak atomic.Int32
	entered, unblock := make(chan struct{}, 20), make(chan struct{})
	var unblockOnce sync.Once
	unblockAll := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(unblockAll)
	for i := 0; i < 20; i++ {
		ctx := &thumbnailObservedContext{Context: context.Background(), ready: make(chan struct{})}
		go func(i int) {
			// 不同缓存实例也必须共用全局配额。
			results <- thumbnailTestCache().do(ctx, fmt.Sprint(i), func() error {
				n := active.Add(1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				entered <- struct{}{}
				<-unblock
				active.Add(-1)
				return nil
			})
		}(i)
		thumbnailWait(t, ctx.ready)
	}
	thumbnailWait(t, entered)
	if active.Load() != 1 || len(thumbnailSlots) != 1 {
		t.Error("全局配额未限制不同原图的生成")
	}
	unblockAll()
	for i := 0; i < 20; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 1 || active.Load() != 0 {
		t.Fatalf("全局解码并发不是1：%d", peak.Load())
	}
}

func TestThumbnailCanceledWaiterAndRetry(t *testing.T) {
	cache := thumbnailTestCache()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- cache.do(context.Background(), "a", func() error {
			close(started)
			<-release
			return errThumbnailInvalid
		})
	}()
	thumbnailWait(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.do(ctx, "a", func() error { t.Error("取消的等待者执行了生成"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("等待者无法取消：%v", err)
	}
	close(release)
	if err := <-done; !errors.Is(err, errThumbnailInvalid) {
		t.Fatal(err)
	}
	if err := cache.do(context.Background(), "a", func() error { return nil }); err != nil {
		t.Fatalf("失败后不能重试：%v", err)
	}
}

func TestThumbnailConcurrentDiskCache(t *testing.T) {
	dir := t.TempDir()
	original := thumbnailTestImage(t, "jpeg", 2048, 1536)
	name := thumbnailTestWrite(t, dir, "202610/a.jpg", original)
	cache := thumbnailTestCache()
	start := make(chan struct{})
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			<-start
			f, err := cache.open(context.Background(), dir, "/uploads/202610/a.jpg")
			if err == nil {
				data, readErr := io.ReadAll(f)
				f.Close()
				if readErr != nil {
					err = readErr
				} else if _, decodeErr := jpeg.Decode(bytes.NewReader(data)); decodeErr != nil {
					err = decodeErr
				}
			}
			results <- err
		}()
	}
	close(start)
	for i := 0; i < 20; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(name))
	if len(entries) != 2 {
		t.Fatalf("原子生成留下临时文件：%v", entries)
	}
	got, _ := os.ReadFile(name)
	if !bytes.Equal(original, got) {
		t.Fatal("并发生成修改了原图")
	}
}

func TestThumbnailPinnedDirectoryRejectsReplacementSymlink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	data := thumbnailTestImage(t, "jpeg", 32, 16)
	thumbnailTestWrite(t, dir, "nested/a.jpg", data)
	outsideFile := thumbnailTestWrite(t, outside, "a.jpg", data)
	root, err := thumbnailDirectory(dir, "nested/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(filepath.Join(dir, "nested"), filepath.Join(dir, "retired")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "nested")); err != nil {
		t.Fatal(err)
	}
	if err := generateThumbnail(root, "a.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "retired", "a.jpg"+thumbnailSuffix)); err != nil {
		t.Fatal("缓存没有写入固定的原父目录")
	}
	if _, err := os.Stat(outsideFile + thumbnailSuffix); !os.IsNotExist(err) {
		t.Fatal("检查后更换链接导致外部写入")
	}
	got, _ := os.ReadFile(outsideFile)
	if !bytes.Equal(got, data) {
		t.Fatal("外部原图被修改")
	}
}

func TestThumbnailCorruptCacheAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	data := thumbnailTestImage(t, "jpeg", 32, 16)
	thumbnailTestWrite(t, dir, "a.jpg", data)
	cacheName := thumbnailTestWrite(t, dir, "a.jpg"+thumbnailSuffix, []byte("invalid cache"))
	if _, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/a.jpg"); !errors.Is(err, errThumbnailInvalid) {
		t.Fatalf("坏缓存未明确失败：%v", err)
	}
	if err := os.Truncate(cacheName, thumbnailMaxCacheBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/a.jpg"); !errors.Is(err, errThumbnailInvalid) {
		t.Fatalf("缓存体积上限未生效：%v", err)
	}
	// 原图及目标缓存名都合法，只有临时文件名超过文件系统单组件上限。
	longName := strings.Repeat("a", 226) + ".jpg"
	originalName := thumbnailTestWrite(t, dir, longName, data)
	_, err := thumbnailTestCache().open(context.Background(), dir, "/uploads/"+longName)
	if err == nil {
		t.Fatal("预期临时文件创建失败")
	}
	if status, _, _ := thumbnailErrorResponse(err); status != http.StatusInternalServerError {
		t.Fatalf("写入失败返回状态错误：%d %v", status, err)
	}
	if _, err := os.Stat(originalName + thumbnailSuffix); !os.IsNotExist(err) {
		t.Fatal("写入失败发布了缓存")
	}
	got, _ := os.ReadFile(originalName)
	if !bytes.Equal(got, data) {
		t.Fatal("缓存写失败改变了原图")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatal("缓存失败留下了临时文件")
	}
}

func thumbnailTestRouter(dir string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/media/thumbnail", func(c *gin.Context) { serveMediaThumbnail(c, dir) })
	s := &Server{}
	r.POST("/api/upload", func(c *gin.Context) { s.uploadEvidenceTo(c, dir) })
	return r
}

func TestThumbnailHTTPHeadersAndErrors(t *testing.T) {
	dir := t.TempDir()
	thumbnailTestWrite(t, dir, "a.jpg", thumbnailTestImage(t, "jpeg", 800, 600))
	thumbnailTestWrite(t, dir, "broken.png", []byte("broken"))
	r := thumbnailTestRouter(dir)
	request := func(target, modified string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if modified != "" {
			req.Header.Set("If-Modified-Since", modified)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := request(thumbnailURL("/uploads/a.jpg"), "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != thumbnailCacheHeader || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("响应头错误：%d %v %s", w.Code, w.Header(), w.Body)
	}
	if _, err := jpeg.Decode(w.Body); err != nil {
		t.Fatal(err)
	}
	modified := w.Header().Get("Last-Modified")
	if modified == "" || request(thumbnailURL("/uploads/a.jpg"), modified).Code != http.StatusNotModified {
		t.Fatal("条件缓存未生效")
	}
	for _, tc := range []struct {
		target string
		status int
		code   string
	}{
		{"/api/media/thumbnail", 404, "thumbnail_not_found"},
		{thumbnailURL("/uploads/absent.jpg"), 404, "thumbnail_not_found"},
		{thumbnailURL("/uploads/../outside.jpg"), 404, "thumbnail_not_found"},
		{thumbnailURL("/uploads/%2e%2e/a.jpg"), 404, "thumbnail_not_found"},
		{thumbnailURL("/uploads/a.jpg") + "&path=%2Fuploads%2Fother.jpg", 404, "thumbnail_not_found"},
		{thumbnailURL("/uploads/a.heic"), 415, "thumbnail_unsupported"},
		{thumbnailURL("/uploads/a.webp"), 415, "thumbnail_unsupported"},
		{thumbnailURL("/uploads/broken.png"), 415, "thumbnail_invalid_image"},
	} {
		w := request(tc.target, "")
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || body["code"] != tc.code || body["error"] == "" || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), dir) {
			t.Fatalf("失败响应错误：%s，%d %s", tc.target, w.Code, w.Body)
		}
	}
}

func TestUploadThumbnailCacheFailureStillSucceeds(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	data := thumbnailTestImage(t, "jpeg", 800, 600)
	outsideFile := thumbnailTestWrite(t, outside, "a.jpg", data)
	r := thumbnailTestRouter(dir)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	thumbnailSlots <- struct{}{}
	var once sync.Once
	release := func() { once.Do(func() { <-thumbnailSlots }) }
	t.Cleanup(release)
	ctx := &thumbnailObservedContext{Context: context.Background(), ready: make(chan struct{})}
	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body).WithContext(ctx)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w, done := httptest.NewRecorder(), make(chan struct{})
	go func() { r.ServeHTTP(w, req); close(done) }()
	thumbnailWait(t, ctx.ready)
	months, err := os.ReadDir(dir)
	if err != nil || len(months) != 1 {
		t.Fatal("原图尚未保存")
	}
	monthDir := filepath.Join(dir, months[0].Name())
	files, err := os.ReadDir(monthDir)
	if err != nil || len(files) != 1 {
		t.Fatal("原图尚未保存")
	}
	original := filepath.Join(monthDir, files[0].Name())
	if err := os.Symlink(outsideFile, original+thumbnailSuffix); err != nil {
		t.Fatal(err)
	}
	release()
	thumbnailWait(t, done)
	var result struct {
		Path       string `json:"path"`
		ThumbURL   string `json:"thumb_url"`
		ThumbError string `json:"thumb_error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Path == "" || result.ThumbURL != "" || result.ThumbError == "" {
		t.Fatalf("缓存失败影响了上传：%d %s", w.Code, w.Body)
	}
	for _, name := range []string{original, outsideFile} {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("缓存失败修改了原图或链接目标")
		}
	}
}

func TestUploadThumbnailSuccessAndFailurePreserveOriginal(t *testing.T) {
	good := thumbnailTestImage(t, "jpeg", 800, 600)
	for _, tc := range []struct {
		name                string
		data                []byte
		preview, thumbError bool
	}{
		{"photo.JPG", good, true, false},
		{"photo.png", []byte("broken"), false, true},
		{"photo.heic", []byte("heic original"), false, true},
		{"photo.webp", []byte("webp original"), false, true},
		{"audio.mp3", []byte("audio original"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			r := thumbnailTestRouter(dir)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(tc.data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var result struct {
				Path       string  `json:"path"`
				Size       int     `json:"size"`
				ThumbURL   *string `json:"thumb_url"`
				ThumbError string  `json:"thumb_error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || result.Size != len(tc.data) || result.ThumbURL == nil || (*result.ThumbURL != "") != tc.preview || (result.ThumbError != "") != tc.thumbError {
				t.Fatalf("上传结果错误：%d %s", w.Code, w.Body)
			}
			rel := strings.TrimPrefix(result.Path, "/uploads/")
			got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
			if err != nil || !bytes.Equal(got, tc.data) {
				t.Fatal("上传失败或原图改变")
			}
			if tc.preview {
				parsed, err := url.Parse(*result.ThumbURL)
				if err != nil || parsed.Path != "/api/media/thumbnail" || parsed.Query().Get("path") != result.Path {
					t.Fatal("thumb_url 未使用原图路径")
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, *result.ThumbURL, nil))
				if w.Code != 200 {
					t.Fatalf("上传缓存不可读取：%d %s", w.Code, w.Body)
				}
			} else if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)) + thumbnailSuffix); !os.IsNotExist(err) {
				t.Fatal("失败或非照片产生了缓存")
			}
		})
	}
}
