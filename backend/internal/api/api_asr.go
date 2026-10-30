package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// MEOW_PUBLIC_URL 需在环境变量配置（线上 https://meow.example.com）：
// ASR 平台要拉公网 URL，音频临时存 uploads 用完即删
func publicURL() string { return os.Getenv("MEOW_PUBLIC_URL") }

// POST /api/asr（multipart file=音频）→ {text}
// 语音输入主通道：孩子按住说话 → 前端转 16kHz WAV → 上传 → 转写 → 文字填框
func (s *Server) asr(c *gin.Context) {
	if !s.LLM.Enabled() || publicURL() == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "语音识别未配置"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file"})
		return
	}
	if fh.Size > 10<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "音频太大（>10MB）"})
		return
	}

	// 存临时文件拿公网 URL（ASR 平台需要能下载）
	dir := filepath.Join(uploadDir(), time.Now().Format("200601"))
	os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("asr_%d.wav", time.Now().UnixNano())
	path := filepath.Join(dir, name)
	if err := c.SaveUploadedFile(fh, path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	audioURL := publicURL() + "/uploads/" + time.Now().Format("200601") + "/" + name

	// 聊天请求仍有服务端时间上限，不沿用后台审核的五分钟窗口。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	text, err := s.LLM.Transcribe(ctx, audioURL)
	os.Remove(path) // 转写完即删，音频不留存（隐私）
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "猫没听清，再试一次喵～"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}
