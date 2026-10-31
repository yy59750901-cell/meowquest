package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// 接入方差异只在 provider 层收敛：业务只描述"要问什么"，不关心是谁家的协议。
const (
	// KindOpenAI OpenAI 兼容协议（通义 compatible-mode、以及任何同类网关）。
	KindOpenAI = "openai"
	// KindZhipu 智谱原生 paas/v4：强制思考，思考与正文共享 max_tokens。
	KindZhipu = "zhipu"
)

// DefaultZhipuBaseURL 智谱原生接口地址。
const DefaultZhipuBaseURL = "https://open.bigmodel.cn/api/paas/v4"

// ErrTruncated 模型在预算内没写完，正文不可信，调用方不能把半截内容当结论。
var ErrTruncated = errors.New("AI 输出被截断（思考或正文超出 max_tokens），未产生完整结论")

// Provider 一个"接入方 + 模型"的组合。
type Provider struct {
	Kind            string // openai / zhipu
	BaseURL         string
	APIKey          string
	Model           string
	MaxTokens       int    // 0 表示由调用方每次指定
	ReasoningEffort string // 仅 zhipu：low/high/max
}

// Enabled 配置了密钥与地址才可用。
func (p Provider) Enabled() bool { return p.APIKey != "" && p.BaseURL != "" && p.Model != "" }

func (p Provider) endpoint() string {
	return strings.TrimSuffix(p.BaseURL, "/") + "/chat/completions"
}

// Part 一条消息内容块。
type Part struct {
	Kind string // text / image
	Text string
	URL  string
}

// TextPart 文本块。
func TextPart(text string) Part { return Part{Kind: "text", Text: text} }

// ImagePart 图片块，URL 可以是 http(s) 地址或 data:image/...;base64, 形式。
func ImagePart(url string) Part { return Part{Kind: "image", URL: url} }

// Usage 上游用量与结束原因，便于诊断截断与成本。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	FinishReason     string
}

// buildBody 按接入方拼请求体，差异只在这里。
func (p Provider) buildBody(parts []Part, maxTokens int, stream bool) map[string]any {
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if part.Kind == "image" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": part.URL}})
			continue
		}
		content = append(content, map[string]any{"type": "text", "text": part.Text})
	}
	body := map[string]any{
		"model":    p.Model,
		"messages": []map[string]any{{"role": "user", "content": content}},
		"stream":   stream,
	}
	tokens := maxTokens
	if tokens <= 0 {
		tokens = p.MaxTokens
	}
	if tokens > 0 {
		body["max_tokens"] = tokens
	}
	switch p.Kind {
	case KindZhipu:
		// 智谱 5.3 系列强制开启思考且不可关闭，思考占用同一个 max_tokens 预算。
		body["thinking"] = map[string]any{"type": "enabled"}
		if p.ReasoningEffort != "" {
			body["reasoning_effort"] = p.ReasoningEffort
		}
	default:
		// 通义等兼容协议默认带思考链，验收/陪聊场景关闭以降低延迟。
		body["enable_thinking"] = false
	}
	return body
}

// Complete 非流式调用，返回正文与用量。
func (p Provider) Complete(ctx context.Context, parts []Part, maxTokens int) (string, Usage, error) {
	if !p.Enabled() {
		return "", Usage{}, errors.New("LLM 未配置（缺少 BaseURL/APIKey/Model）")
	}
	b, err := json.Marshal(p.buildBody(parts, maxTokens, false))
	if err != nil {
		return "", Usage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(b))
	if err != nil {
		return "", Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", Usage{}, p.decodeError(resp.Body, resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return "", Usage{}, err
	}
	usage := Usage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens}
	if len(out.Choices) > 0 {
		usage.FinishReason = out.Choices[0].FinishReason
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", usage, errors.New(redact(out.Error.Message, p.APIKey))
	}
	if len(out.Choices) == 0 {
		return "", usage, errors.New("AI 没有返回任何结果")
	}
	if usage.FinishReason == "length" {
		return "", usage, ErrTruncated
	}
	return out.Choices[0].Message.Content, usage, nil
}

// decodeError 解析上游错误体，脱敏后再返回，避免把密钥或签名 URL 带进日志。
func (p Provider) decodeError(body io.Reader, status int) error {
	var failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(body, 8192)).Decode(&failure)
	code, message := failure.Code, failure.Message
	if failure.Error != nil {
		code, message = failure.Error.Code, failure.Error.Message
	}
	return fmt.Errorf("llm http %d %s: %s", status, code, redact(message, p.APIKey))
}

// redact 抹掉正文里的密钥，并限制长度。
func redact(message, apiKey string) string {
	if apiKey != "" {
		message = strings.ReplaceAll(message, apiKey, "[redacted]")
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	return message
}

// ProviderFromEnv 读取 MEOW_<prefix>_* 配置；未显式配置时回落到 fallback（保持旧行为）。
func ProviderFromEnv(prefix string, fallback Provider) Provider {
	base := os.Getenv("MEOW_" + prefix + "_BASE_URL")
	key := os.Getenv("MEOW_" + prefix + "_API_KEY")
	model := os.Getenv("MEOW_" + prefix + "_MODEL")
	if base == "" && key == "" && model == "" && os.Getenv("MEOW_"+prefix+"_PROVIDER") == "" {
		return fallback
	}
	kind := strings.ToLower(strings.TrimSpace(os.Getenv("MEOW_" + prefix + "_PROVIDER")))
	if kind == "" {
		kind = fallback.Kind
	}
	if kind == "" {
		kind = KindOpenAI
	}
	if base == "" {
		if kind == KindZhipu {
			base = DefaultZhipuBaseURL
		} else {
			base = fallback.BaseURL
		}
	}
	if key == "" {
		key = fallback.APIKey
	}
	if model == "" {
		model = fallback.Model
	}
	return Provider{
		Kind:            kind,
		BaseURL:         base,
		APIKey:          key,
		Model:           model,
		MaxTokens:       envInt("MEOW_"+prefix+"_MAX_TOKENS", fallback.MaxTokens),
		ReasoningEffort: os.Getenv("MEOW_" + prefix + "_REASONING_EFFORT"),
	}
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return v
	}
	return def
}
