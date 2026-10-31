package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// defaultASRModel 非实时整段音频转写模型。
const defaultASRModel = "qwen-audio-3.0-asr-flash-filetrans"

// asrBase filetrans 系列走原生 /api/v1。
func asrBase() string {
	if v := os.Getenv("MEOW_ASR_BASE_URL"); v != "" {
		return v
	}
	return "https://cn-beijing.maas.qianwenaiapi.com/api/v1"
}

// asrConfig 转写单独取配置，未显式配置时回落到对话侧，保持旧行为。
func (c *Client) asrConfig() (baseURL, apiKey, model string) {
	baseURL, apiKey, model = c.ASRBaseURL, c.ASRAPIKey, c.ASRModel
	if baseURL == "" {
		baseURL = asrBase()
	}
	if apiKey == "" {
		apiKey = c.APIKey
	}
	if model == "" {
		model = defaultASRModel
	}
	return baseURL, apiKey, model
}

// TranscriptionState 可持久化的上游任务与结果，避免网络失败或重启后重复提交。
type TranscriptionState struct {
	TaskID        string `json:"task_id,omitempty"`
	TranscriptURL string `json:"transcript_url,omitempty"`
	Text          string `json:"text,omitempty"`
	Done          bool   `json:"done,omitempty"`
	Failed        bool   `json:"failed,omitempty"`
}

func (c *Client) Transcribe(ctx context.Context, audioURL string) (string, error) {
	return c.TranscribeCached(ctx, audioURL, &TranscriptionState{}, nil)
}

// TranscribeCached 轮询到调用方 context 截止；每次获得 task ID 或结果立即持久化。
func (c *Client) TranscribeCached(ctx context.Context, audioURL string, state *TranscriptionState, save func() error) (string, error) {
	if c == nil || !c.Enabled() {
		return "", fmt.Errorf("LLM 未配置")
	}
	persist := func() error {
		if save != nil {
			return save()
		}
		return nil
	}
	if state.Done {
		return state.Text, nil
	}
	if state.Failed {
		*state = TranscriptionState{}
	}
	if state.TaskID == "" {
		asrURL, asrKey, asrModel := c.asrConfig()
		body, _ := json.Marshal(map[string]any{
			"model": asrModel,
			"input": map[string]any{"file_urls": []string{audioURL}},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, asrURL+"/services/audio/asr/transcription", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+asrKey)
		req.Header.Set("X-DashScope-Async", "enable")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("asr submit: %w", err)
		}
		var submit struct {
			Output struct {
				TaskID string `json:"task_id"`
			} `json:"output"`
			Message string `json:"message"`
		}
		err = json.NewDecoder(resp.Body).Decode(&submit)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("asr submit: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || submit.Output.TaskID == "" {
			return "", fmt.Errorf("asr submit failed (%d): %s", resp.StatusCode, submit.Message)
		}
		state.TaskID = submit.Output.TaskID
		if err := persist(); err != nil {
			return "", err
		}
	}

	if state.TranscriptURL == "" {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ticker.C:
			}
			asrURL, asrKey, _ := c.asrConfig()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, asrURL+"/tasks/"+url.PathEscape(state.TaskID), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Authorization", "Bearer "+asrKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				continue
			}
			var poll struct {
				Output struct {
					TaskStatus string `json:"task_status"`
					Results    []struct {
						TranscriptionURL string `json:"transcription_url"`
						SubtaskStatus    string `json:"subtask_status"`
					} `json:"results"`
				} `json:"output"`
				Message string `json:"message"`
			}
			err = json.NewDecoder(resp.Body).Decode(&poll)
			resp.Body.Close()
			if err != nil {
				return "", fmt.Errorf("asr poll: %w", err)
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return "", fmt.Errorf("asr poll failed (%d): %s", resp.StatusCode, poll.Message)
			}
			switch poll.Output.TaskStatus {
			case "SUCCEEDED":
				if len(poll.Output.Results) == 0 || poll.Output.Results[0].TranscriptionURL == "" {
					state.Failed = true
					if err := persist(); err != nil {
						return "", err
					}
					return "", fmt.Errorf("asr no result")
				}
				state.TranscriptURL = poll.Output.Results[0].TranscriptionURL
			case "FAILED", "CANCELED":
				state.Failed = true
				if err := persist(); err != nil {
					return "", err
				}
				return "", fmt.Errorf("asr failed: %s", poll.Message)
			}
			if state.TranscriptURL != "" {
				if err := persist(); err != nil {
					return "", err
				}
				break
			}
		}
	}
	text, err := fetchTranscript(ctx, state.TranscriptURL)
	if err != nil {
		return "", err
	}
	state.Text, state.Done = text, true
	if err := persist(); err != nil {
		return "", err
	}
	return text, nil
}

func fetchTranscript(ctx context.Context, transcriptURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, transcriptURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 转写结果 URL 可能带签名，错误展示不能泄露 URL 查询参数。
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			return "", fmt.Errorf("asr transcript fetch: %w", requestErr.Err)
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asr transcript http %d", resp.StatusCode)
	}
	var out struct {
		Transcripts []struct {
			Text string `json:"text"`
		} `json:"transcripts"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&out); err != nil {
		return "", err
	}
	texts := make([]string, 0, len(out.Transcripts))
	for _, transcript := range out.Transcripts {
		texts = append(texts, transcript.Text)
	}
	text := strings.Join(texts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("empty transcript")
	}
	return text, nil
}
