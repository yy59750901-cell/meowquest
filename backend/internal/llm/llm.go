package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client OpenAI 兼容 LLM 客户端（qwen3.5-flash / 任何 compatible-mode 服务）
type Client struct {
	BaseURL string // https://cn-beijing.maas.qianwenaiapi.com/compatible-mode/v1
	APIKey  string
	Model   string
}

// NewFromEnv 从环境变量构建（MEOW_LLM_BASE_URL/KEY/MODEL）
func NewFromEnv() *Client {
	return &Client{
		BaseURL: envOr("MEOW_LLM_BASE_URL", "https://cn-beijing.maas.qianwenaiapi.com/compatible-mode/v1"),
		APIKey:  os.Getenv("MEOW_LLM_API_KEY"),
		Model:   envOr("MEOW_LLM_MODEL", "qwen3.5-flash"),
	}
}

func (c *Client) Enabled() bool { return c.APIKey != "" }

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChat 流式对话：messages → onDelta(增量文本)；返回完整回复
func (c *Client) StreamChat(ctx context.Context, messages []Msg, onDelta func(string)) (string, error) {
	if !c.Enabled() {
		return "", errors.New("LLM 未配置（MEOW_LLM_API_KEY）")
	}
	body := map[string]any{
		"model":    c.Model,
		"messages": messages,
		"stream":   true,
		// qwen3.5 默认带思考链（延迟高、浪费 token），陪聊场景关闭
		"enable_thinking": false,
		"max_tokens":      300,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e map[string]any
		json.NewDecoder(resp.Body).Decode(&e)
		return "", fmt.Errorf("llm http %d: %v", resp.StatusCode, e)
	}

	// SSE: "data: {...}" 行流；delta.content 为增量
	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			delta := chunk.Choices[0].Delta.Content
			full.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
	}
	return full.String(), scanner.Err()
}

// ChatOnce 非流式调用（验收判定等不需要打字机的场景）
func (c *Client) ChatOnce(ctx context.Context, messages []Msg) (string, error) {
	return c.StreamChat(ctx, messages, nil)
}

// ChatOnceVision 带图片的多模态非流式调用（qwen vision 格式）
func (c *Client) ChatOnceVision(ctx context.Context, text, imageURL string) (string, error) {
	body := map[string]any{
		"model": c.Model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": text},
				{"type": "image_url", "image_url": map[string]string{"url": imageURL}},
			},
		}},
		"enable_thinking": false,
		"max_tokens":      300,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Error != nil {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("empty response")
	}
	return out.Choices[0].Message.Content, nil
}

// LLMVisionMulti 多图综合判定（qwen 兼容：content 数组多 image_url）
func LLMVisionMulti(ctx context.Context, baseURL, apiKey, model, text string, imageUrls []string) (string, error) {
	content := []map[string]any{{"type": "text", "text": text}}
	for _, u := range imageUrls {
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": u}})
	}
	body := map[string]any{
		"model":           model,
		"messages":        []map[string]any{{"role": "user", "content": content}},
		"enable_thinking": false,
		"max_tokens":      400,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Error   *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&failure)
		code, message := failure.Code, failure.Message
		if failure.Error != nil {
			code, message = failure.Error.Code, failure.Error.Message
		}
		if apiKey != "" {
			message = strings.ReplaceAll(message, apiKey, "[redacted]")
		}
		if len(message) > 1000 {
			message = message[:1000]
		}
		return "", fmt.Errorf("llm http %d %s: %s", resp.StatusCode, code, message)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("empty response")
	}
	return out.Choices[0].Message.Content, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
