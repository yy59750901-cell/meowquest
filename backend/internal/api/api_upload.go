package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// POST /api/upload 任务证据上传（multipart，孩子端）
// 拍照/录音走 <input type="file">（HTTP 局域网下 getUserMedia 不可用）
func (s *Server) uploadEvidence(c *gin.Context) {
	s.uploadEvidenceTo(c, uploadDir())
}

func (s *Server) uploadEvidenceTo(c *gin.Context, baseDir string) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	if file.Size > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件太大（限 20MB）"})
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".webm", ".mp3", ".mp4", ".m4a", ".aac", ".wav", ".ogg", ".mov":
	default:
		ext = filepath.Ext(file.Filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型: " + ext})
		return
	}
	// uploads/<日期目录>/<时间戳><ext>
	month := time.Now().Format("200601")
	dir := filepath.Join(baseDir, month)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	name := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, name)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rel := "/uploads/" + month + "/" + name
	result := gin.H{"path": rel, "size": file.Size, "thumb_url": ""}
	// 原图已保存，预览失败只返回提示，不改变上传成功或原图路径。
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic":
		if preview, err := evidenceThumbnails.open(c.Request.Context(), baseDir, rel); err == nil {
			preview.Close()
			result["thumb_url"] = thumbnailURL(rel)
		} else {
			_, _, message := thumbnailErrorResponse(err)
			result["thumb_error"] = message
		}
	}
	c.JSON(http.StatusOK, result)
}

// uploadDir 上传目录（可执行文件同目录下 uploads，与 cwd 无关）
func uploadDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "./uploads"
	}
	return filepath.Join(filepath.Dir(exe), "uploads")
}
