package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// asrBase filetrans 系列走原生 /api/v1（与 chat 的 compatible-mode 前缀不同）
func asrBase() string {
	if v := os.Getenv("MEOW_ASR_BASE_URL"); v != "" {
		return v
	}
	return "https://cn-beijing.maas.qianwenaiapi.com/api/v1"
}

// Transcribe 文件转写：音频公网 URL → 提交 filetrans 异步任务 → 轮询 → 返回文字
// 适合"按住说完松开"的非实时场景（延迟 3~6 秒）
func (c *Client) Transcribe(ctx context.Context, audioURL string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("LLM 未配置")
	}
	// 1. 提交任务
	body := map[string]any{
		"model": "qwen-audio-3.0-asr-flash-filetrans",
		"input": map[string]any{"file_urls": []string{audioURL}},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", asrBase()+"/services/audio/asr/transcription", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("X-DashScope-Async", "enable") // 异步任务标志（漏了会报 does not support synchronous calls）

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("asr submit: %w", err)
	}
	defer resp.Body.Close()
	var submit struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&submit)
	if submit.Output.TaskID == "" {
		return "", fmt.Errorf("asr submit failed: %s", submit.Message)
	}

	// 2. 轮询（最多 15s，一句话通常 2~4s）
	taskURL := asrBase() + "/tasks/" + submit.Output.TaskID
	for i := 0; i < 15; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
		r2, _ := http.NewRequestWithContext(ctx, "GET", taskURL, nil)
		r2.Header.Set("Authorization", "Bearer "+c.APIKey)
		resp2, err := http.DefaultClient.Do(r2)
		if err != nil {
			continue
		}
		var poll struct {
			Output struct {
				TaskStatus string `json:"task_status"`
				Results    []struct {
					TranscriptionURL string `json:"transcription_url"`
				} `json:"results"`
			} `json:"output"`
			Message string `json:"message"`
		}
		json.NewDecoder(resp2.Body).Decode(&poll)
		resp2.Body.Close()
		switch poll.Output.TaskStatus {
		case "SUCCEEDED":
			if len(poll.Output.Results) == 0 || poll.Output.Results[0].TranscriptionURL == "" {
				return "", fmt.Errorf("asr no result")
			}
			return fetchTranscript(poll.Output.Results[0].TranscriptionURL)
		case "FAILED":
			return "", fmt.Errorf("asr failed: %s", poll.Message)
		}
	}
	return "", fmt.Errorf("asr timeout")
}

// fetchTranscript 拉转写结果 JSON → transcripts[0].text
func fetchTranscript(url string) (string, error) {
	r3, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer r3.Body.Close()
	var out struct {
		Transcripts []struct {
			Text string `json:"text"`
		} `json:"transcripts"`
	}
	if err := json.NewDecoder(io.LimitReader(r3.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Transcripts) == 0 {
		return "", fmt.Errorf("empty transcript")
	}
	return out.Transcripts[0].Text, nil
}
