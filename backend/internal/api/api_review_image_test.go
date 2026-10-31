package api

import (
	"encoding/json"
	"errors"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

// AI 验收用的 768px 档位：原图太大接入方下载超时，480px 预览又可能看不清书页小字。
func TestReviewImageVariantWhitelist(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		edge   int
		suffix string
	}{
		{"缺省走预览", nil, thumbnailEdge, thumbnailSuffix},
		{"空值走预览", []string{""}, thumbnailEdge, thumbnailSuffix},
		{"显式 480", []string{"480"}, thumbnailEdge, thumbnailSuffix},
		{"显式 768", []string{"768"}, reviewImageEdge, reviewImageSuffix},
	} {
		v, err := thumbnailVariantOf(tc.values)
		if err != nil {
			t.Fatalf("%s：%v", tc.name, err)
		}
		if v.edge != tc.edge || v.suffix != tc.suffix {
			t.Fatalf("%s：得到 %d/%s，想要 %d/%s", tc.name, v.edge, v.suffix, tc.edge, tc.suffix)
		}
	}
	// 尺寸只认白名单，绝不把 query 里的数字当缩放参数：任意尺寸会让磁盘缓存无限膨胀。
	for _, bad := range [][]string{{"1024"}, {"7680"}, {"0"}, {"-768"}, {"abc"}, {"768", "768"}, {"480", "768"}} {
		if _, err := thumbnailVariantOf(bad); !errors.Is(err, errThumbnailBadSize) {
			t.Fatalf("尺寸 %v 应被拒绝，实际 %v", bad, err)
		}
	}
}

func TestReviewImageSeparateCachePerSize(t *testing.T) {
	dir := t.TempDir()
	thumbnailTestWrite(t, dir, "a.jpg", thumbnailTestImage(t, "jpeg", 1600, 1200))
	r := thumbnailTestRouter(dir)
	get := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w
	}
	decode := func(t *testing.T, w *httptest.ResponseRecorder) (int, int) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("状态码 %d：%s", w.Code, w.Body)
		}
		img, err := jpeg.Decode(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		return img.Bounds().Dx(), img.Bounds().Dy()
	}

	// 两条尺寸互不覆盖：各自落一份缓存文件，且尺寸正确。
	if w, h := decode(t, get(thumbnailURL("/uploads/a.jpg"))); w != 480 || h != 360 {
		t.Fatalf("预览尺寸应为 480x360，实际 %dx%d", w, h)
	}
	if w, h := decode(t, get(reviewImageURL("/uploads/a.jpg"))); w != 768 || h != 576 {
		t.Fatalf("验收尺寸应为 768x576，实际 %dx%d", w, h)
	}
	for _, suffix := range []string{thumbnailSuffix, reviewImageSuffix} {
		if _, err := os.Stat(filepath.Join(dir, "a.jpg"+suffix)); err != nil {
			t.Fatalf("缺少 %s 缓存：%v", suffix, err)
		}
	}
	// 小图不放大：480px 的源不能凭空拉成 768。
	small := t.TempDir()
	thumbnailTestWrite(t, small, "s.jpg", thumbnailTestImage(t, "jpeg", 600, 400))
	rSmall := thumbnailTestRouter(small)
	w := httptest.NewRecorder()
	rSmall.ServeHTTP(w, httptest.NewRequest(http.MethodGet, reviewImageURL("/uploads/s.jpg"), nil))
	img, err := jpeg.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 600 || img.Bounds().Dy() != 400 {
		t.Fatalf("小图不应放大，实际 %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestReviewImageBadSizeAndCacheNameRejected(t *testing.T) {
	dir := t.TempDir()
	thumbnailTestWrite(t, dir, "a.jpg", thumbnailTestImage(t, "jpeg", 1600, 1200))
	r := thumbnailTestRouter(dir)
	for _, tc := range []struct {
		target string
		status int
		code   string
	}{
		{thumbnailURL("/uploads/a.jpg") + "&size=1024", http.StatusBadRequest, "thumbnail_bad_size"},
		{thumbnailURL("/uploads/a.jpg") + "&size=abc", http.StatusBadRequest, "thumbnail_bad_size"},
		{thumbnailURL("/uploads/a.jpg") + "&size=768&size=768", http.StatusBadRequest, "thumbnail_bad_size"},
		// 缓存文件不能作为源图，防止缓存套缓存。
		{thumbnailURL("/uploads/a.jpg" + reviewImageSuffix), http.StatusNotFound, "thumbnail_not_found"},
		{reviewImageURL("/uploads/a.jpg" + thumbnailSuffix), http.StatusNotFound, "thumbnail_not_found"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s：响应不是 JSON：%s", tc.target, w.Body)
		}
		if w.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("%s：得到 %d/%s，想要 %d/%s", tc.target, w.Code, body["code"], tc.status, tc.code)
		}
	}
}

// 验收提示词用的图片 URL 必须指向 768 档，而不是家长预览的 480 档或原图。
func TestReviewImageURLTargetsReviewVariant(t *testing.T) {
	target := reviewImageURL("/uploads/202610/a.jpg")
	if want := "/api/media/thumbnail?path=%2Fuploads%2F202610%2Fa.jpg&size=" + strconv.Itoa(reviewImageEdge); target != want {
		t.Fatalf("验收图片 URL 错误：%s", target)
	}
	if thumbnailURL("/uploads/202610/a.jpg") == target {
		t.Fatal("验收图片 URL 不应与预览 URL 相同")
	}
}

// 路由直连校验：两个尺寸共用同一个 handler，靠 size 分流。
func TestReviewImageRouteServesBothSizes(t *testing.T) {
	dir := t.TempDir()
	thumbnailTestWrite(t, dir, "a.jpg", thumbnailTestImage(t, "jpeg", 1600, 1200))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/media/thumbnail", func(c *gin.Context) { serveMediaThumbnail(c, dir) })
	for _, tc := range []struct {
		target string
		edge   int
	}{
		{thumbnailURL("/uploads/a.jpg"), thumbnailEdge},
		{reviewImageURL("/uploads/a.jpg"), reviewImageEdge},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s：%d %s", tc.target, w.Code, w.Body)
		}
		img, err := jpeg.Decode(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != tc.edge {
			t.Fatalf("%s：宽度应为 %d，实际 %d", tc.target, tc.edge, img.Bounds().Dx())
		}
	}
}
