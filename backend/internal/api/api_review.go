package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

// ===== AI 验收（b 模式）：证据 → qwen3.5-flash 判定（vision 看图 / ASR 转写听录音）→ 结论进家长确认队列 =====

type reviewVerdict struct {
	Pass        bool   `json:"pass"`
	Score       int    `json:"score"`        // 理解度 1-5（score 模式）
	Reason      string `json:"reason"`       // 给家长
	KidFeedback string `json:"kid_feedback"` // 给孩子（猫说）
}

// runReview 的长调用均在事务外，进度和上游任务缓存由租约持有者落库。
func (s *Server) runReview(ctx context.Context, dt store.DailyTask, comp EvidenceRow, cache map[string]*llm.TranscriptionState, report func(string) error) (reviewVerdict, error) {
	if err := report("preparing"); err != nil {
		return reviewVerdict{}, err
	}
	client := s.LLM
	if client == nil || !client.Enabled() {
		return reviewVerdict{}, fmt.Errorf("llm 未配置")
	}

	var snap struct {
		Name         string `json:"name"`
		ReviewPrompt string `json:"review_prompt"`
		ConfirmMode  string `json:"confirm_mode"`
		AnalysisMode string `json:"analysis_mode"`
	}
	if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
		return reviewVerdict{}, err
	}
	// job 创建时已冻结旧快照兼容字段，执行和重试不再读取可变模板。

	prompt := fmt.Sprintf(`你是儿童任务的验收助手（后台判定，孩子看不到你的原文）。任务「%s」。
验收要求：%s
请只输出一行 JSON：{"pass": true 或 false, "reason": "给家长看的一句话说明（20字内）"}
判定宽松原则：这是孩子的努力，只要基本达标就 pass；明显没做/内容完全不符才 false。`,
		snap.Name,
		orDefault(snap.ReviewPrompt, "提交的证据是否体现孩子认真完成了任务"))

	// 证据收集：多图 + 多录音
	evList := parseEvidenceJSON(comp.Evidence)
	var photoURLs []string
	var audioURLs []string
	for _, e := range evList {
		switch e.Type {
		case "photo":
			photoURLs = append(photoURLs, publicURL()+e.Path)
		case "audio":
			audioURLs = append(audioURLs, publicURL()+e.Path)
		}
	}

	// 每条提交录音都必须成功转写，完整文本不按字节截断。
	var transcriptList []string
	for _, au := range audioURLs {
		state := cache[au]
		if state == nil {
			state = &llm.TranscriptionState{}
			cache[au] = state
		}
		if err := report("transcribing"); err != nil {
			return reviewVerdict{}, err
		}
		text, err := client.TranscribeCached(ctx, au, state, func() error { return report("transcribing") })
		if err != nil {
			return reviewVerdict{}, fmt.Errorf("录音转写失败: %w", err)
		}
		transcriptList = append(transcriptList, text)
	}
	transcripts := strings.Join(transcriptList, "\n")
	if err := report("reviewing"); err != nil {
		return reviewVerdict{}, err
	}

	// 分析模式：score 输出星级+双反馈
	if snap.AnalysisMode == "score" {
		prompt = prompt + `
这是"理解度分析"模式：额外输出 score（1-5 整数，孩子的完成/理解质量）和 kid_feedback（一句以猫的口吻给孩子的鼓励话，可含一个小问题引导思考，30字内，称呼"主人"）。
JSON 格式：{"pass": bool, "score": 1-5, "reason": "给家长（20字内）", "kid_feedback": "给孩子的猫语气话"}`
	}

	var raw string
	var err error
	switch {
	case len(photoURLs) > 0:
		// 多图综合：文本 + 逐图 URL（vision 多图消息）
		// qwen 兼容格式：content 数组多个 image_url
		ask := prompt
		if transcripts != "" {
			ask += "\n孩子还讲了（录音转写）：" + transcripts
		}
		raw, err = llm.LLMVisionMulti(ctx, client.BaseURL, client.APIKey, client.Model, ask, photoURLs)
	case transcripts != "":
		raw, err = llm.LLMVisionMulti(ctx, client.BaseURL, client.APIKey, client.Model, prompt+"\n孩子的录音内容（转写）："+transcripts, nil)
	default:
		// 无媒体证据：用计时时长判定（自由任务家长兜底）
		raw, err = llm.LLMVisionMulti(ctx, client.BaseURL, client.APIKey, client.Model, prompt+"\n（无照片/录音，仅计时 "+fmtDurationSec(comp.DurationSec)+"，无法核实内容时判 false 并注明）", nil)
	}
	if err != nil {
		return reviewVerdict{}, err
	}

	// 解析 JSON（容错：截取第一个 {...}）
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return reviewVerdict{}, fmt.Errorf("bad json: %s", raw[:min(80, len(raw))])
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw[start:end+1]), &fields); err != nil {
		return reviewVerdict{}, err
	}
	var pass *bool
	if err := json.Unmarshal(fields["pass"], &pass); err != nil || pass == nil {
		return reviewVerdict{}, fmt.Errorf("AI 结果缺少有效 pass 字段")
	}
	var v reviewVerdict
	if err := json.Unmarshal([]byte(raw[start:end+1]), &v); err != nil {
		return reviewVerdict{}, err
	}
	if v.Reason == "" {
		v.Reason = "AI 已检查"
	}
	return v, nil
}

type EvidenceRow struct {
	Evidence    string `gorm:"column:evidence"`
	DurationSec int    `gorm:"column:duration_sec"`
}

func parseEvidenceJSON(raw string) []struct {
	Type string
	Path string
} {
	var list []struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	json.Unmarshal([]byte(orDefault(raw, "[]")), &list)
	out := make([]struct {
		Type string
		Path string
	}, 0, len(list))
	for _, e := range list {
		out = append(out, struct {
			Type string
			Path string
		}{Type: e.Type, Path: e.Path})
	}
	return out
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func fmtDurationSec(sec int) string {
	if sec <= 0 {
		return "0 分钟"
	}
	return fmt.Sprintf("%d 分钟", sec/60)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
