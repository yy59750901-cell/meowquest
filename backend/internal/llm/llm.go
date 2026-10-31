package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// Kind 接入方类型（openai/zhipu），决定请求体差异；空值按 openai 兼容处理。
	Kind            string
	MaxTokens       int
	ReasoningEffort string
	// 转写与对话分开配置，换接入方时互不影响。
	ASRBaseURL string
	ASRAPIKey  string
	ASRModel   string
}

// NewFromEnv 从环境变量构建（MEOW_LLM_* 与 MEOW_ASR_*）
func NewFromEnv() *Client {
	apiKey := os.Getenv("MEOW_LLM_API_KEY")
	return &Client{
		BaseURL:         envOr("MEOW_LLM_BASE_URL", "https://cn-beijing.maas.qianwenaiapi.com/compatible-mode/v1"),
		APIKey:          apiKey,
		Model:           envOr("MEOW_LLM_MODEL", "qwen3.5-flash"),
		Kind:            envOr("MEOW_LLM_PROVIDER", KindOpenAI),
		MaxTokens:       envInt("MEOW_LLM_MAX_TOKENS", 300),
		ReasoningEffort: os.Getenv("MEOW_LLM_REASONING_EFFORT"),
		ASRBaseURL:      asrBase(),
		ASRAPIKey:       envOr("MEOW_ASR_API_KEY", apiKey),
		ASRModel:        envOr("MEOW_ASR_MODEL", defaultASRModel),
	}
}

func (c *Client) Enabled() bool { return c.APIKey != "" }

// ChatProvider 把客户端配置暴露成 Provider，供审核等场景复用同一套协议拼装。
func (c *Client) ChatProvider() Provider {
	kind := strings.ToLower(strings.TrimSpace(c.Kind))
	if kind == "" {
		kind = KindOpenAI
	}
	return Provider{Kind: kind, BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model,
		MaxTokens: c.MaxTokens, ReasoningEffort: c.ReasoningEffort}
}

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChat 流式对话：messages → onDelta(增量文本)；返回完整回复
func (c *Client) StreamChat(ctx context.Context, messages []Msg, onDelta func(string)) (string, error) {
	if !c.Enabled() {
		return "", errors.New("LLM 未配置（MEOW_LLM_API_KEY）")
	}
	// 请求体按接入方拼装（qwen 关闭思考链降低延迟；智谱强制思考）。
	body := c.ChatProvider().buildBody(nil, 0, true)
	body["messages"] = messages
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

// legacyVisionMaxTokens 兼容旧调用方的输出预算。
const legacyVisionMaxTokens = 400

// LLMVisionMulti 多图综合判定：文本 + 逐图（URL 或 base64 均可）。
// 保留旧签名，内部统一走 Provider，新增调用请直接用 Provider.Complete。
func LLMVisionMulti(ctx context.Context, baseURL, apiKey, model, text string, imageUrls []string) (string, error) {
	p := Provider{Kind: KindOpenAI, BaseURL: baseURL, APIKey: apiKey, Model: model}
	parts := []Part{TextPart(text)}
	for _, u := range imageUrls {
		parts = append(parts, ImagePart(u))
	}
	out, _, err := p.Complete(ctx, parts, legacyVisionMaxTokens)
	return out, err
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
