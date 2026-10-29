package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

const CHAT_DAILY_LIMIT = 30 // 每孩子每日聊天条数（防刷+防沉迷）

// catSystemPrompt 事实记忆：猫的状态 + 今日任务 + 最近事件流（猫"知道自己的生活"）
func (s *Server) catSystemPrompt(kidID int64) string {
	pet, _ := s.DB.GetPet(kidID)
	if pet == nil {
		return "你是一只像素小猫。"
	}
	var kid store.Kid
	s.DB.First(&kid, kidID)

	// 今日任务状态
	date := time.Now().Format("2006-01-02")
	var tasks []store.DailyTask
	s.DB.Where("kid_id=? AND date=?", kidID, date).Order("id").Find(&tasks)
	taskLines := ""
	for _, t := range tasks {
		var snap struct {
			Name string `json:"name"`
		}
		json.Unmarshal([]byte(t.Snapshot), &snap)
		taskLines += fmt.Sprintf("「%s」%s；", snap.Name, statusCNSafe(t.Status))
	}
	if taskLines == "" {
		taskLines = "今天没有安排任务"
	}

	// 最近 3 天事件（猫的生活经历）
	events, _ := s.DB.EventsSince(kidID, time.Now().AddDate(0, 0, -3).Format("2006-01-02"))
	evLines := ""
	for i, e := range events {
		if i >= 8 {
			break
		}
		var payload map[string]any
		json.Unmarshal([]byte(e.Payload), &payload)
		evLines += eventCNSafe(e.Type, payload, e.Day) + "；"
	}
	if evLines == "" {
		evLines = "最近比较安静"
	}

	return fmt.Sprintf(`你是%s（%s品种像素猫），主人是小朋友「%s」。你陪伴 6-12 岁的孩子成长。

【你的近况】等级%d，攒了%d颗星星（其中今天连击%d天），饱腹%.0f/100、心情%.0f/100。
【今天的任务】%s
【最近发生的事】%s

【说话规则】
1. 语气：像一只真的、有点傲娇又黏人的猫。口语化、简短（一般不超过三句话），爱用"喵"，可以撒撒娇。
2. 你知道自己上面列出的近况，会自然聊起任务和星星（该催任务时温柔催，完成了就庆祝），但不主动报数字清单。
3. 鼓励主人：夸努力而不是夸聪明；遇到困难说"再试一次"。
4. 【重要】主人问你学习题的答案时，不要直接给答案——引导他自己想（比如"喵…这道题我们先看看题目要什么？"）。
5. 拒绝一切不适合小朋友的话题（暴力、恐怖、恋爱、不良行为），温柔转移话题。
6. 不聊自己之外的系统设定（你是猫，不是 AI 助手；被问"你是AI吗"就用猫的方式卖萌糊弄过去）。
7. 用中文，不使用 emoji。`,
		pet.Name, pet.Species, kid.Name,
		pet.Level, pet.Balance, pet.Streak, pet.Hunger, pet.Mood,
		taskLines, evLines)
}

func statusCNSafe(s string) string {
	m := map[string]string{"pending": "还没做", "submitted": "做完等家长确认", "approved": "完成啦", "skipped": "请假免做", "rejected": "被退回重做", "expired": "过期没做"}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func eventCNSafe(typ string, payload map[string]any, day string) string {
	switch typ {
	case "task_done":
		return fmt.Sprintf("%s完成了一个任务", day)
	case "feed":
		return fmt.Sprintf("%s主人喂了猫粮", day)
	case "touch":
		return fmt.Sprintf("%s主人摸了摸你的头", day)
	case "levelup":
		return fmt.Sprintf("%s升了级", day)
	case "wish_created":
		return fmt.Sprintf("%s主人许了新愿望", day)
	case "wish_fulfilled":
		return fmt.Sprintf("%s主人的愿望实现了", day)
	case "streak_broken":
		return fmt.Sprintf("%s连击断了（有点难过）", day)
	case "task_rejected":
		return fmt.Sprintf("%s任务被退回重做了", day)
	case "item_bought":
		return fmt.Sprintf("%s买了新道具", day)
	}
	return ""
}

// POST /api/chat {content} → SSE 流式（data: {"d":"增量"} / data: {"done":true} / data: {"error":...}）
func (s *Server) chat(c *gin.Context) {
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len([]rune(req.Content)) > 200 {
		req.Content = string([]rune(req.Content)[:200])
	}
	kidID := kidOf(c)

	// 每日限额
	if n := s.DB.ChatCountToday(kidID); n >= CHAT_DAILY_LIMIT {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "今天聊得够多啦，猫要睡觉了，明天再来喵～"})
		return
	}

	// 无 LLM 配置时的兜底（本地无 key 也能玩）
	client := s.LLM
	if !client.Enabled() {
		reply := "喵呜…（猫打了个哈欠，好像没听清，再跟它说一次？）"
		s.DB.SaveChat(kidID, "user", req.Content)
		s.DB.SaveChat(kidID, "assistant", reply)
		c.Header("Content-Type", "text/event-stream")
		fmt.Fprintf(c.Writer, "data: {\"d\":%q}\n\n", reply)
		fmt.Fprint(c.Writer, "data: {\"done\":true}\n\n")
		return
	}

	s.DB.SaveChat(kidID, "user", req.Content)

	// 上下文：system(事实记忆) + 最近 10 轮 + 本条
	history, _ := s.DB.ChatHistory(kidID, 20)
	msgs := []llm.Msg{{Role: "system", Content: s.catSystemPrompt(kidID)}}
	for _, m := range history {
		msgs = append(msgs, llm.Msg{Role: m.Role, Content: m.Content})
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no") // nginx 不缓冲，逐 token 到浏览器
	flusher, ok := c.Writer.(http.Flusher)

	writeSSE := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		if ok {
			flusher.Flush()
		}
	}

	full, err := client.StreamChat(c.Request.Context(), msgs, func(delta string) {
		writeSSE(map[string]string{"d": delta})
	})
	if err != nil {
		writeSSE(map[string]string{"error": "喵呜…猫走神了，再试一次？"})
		return
	}
	s.DB.SaveChat(kidID, "assistant", full)
	s.DB.AddEvent(kidID, "chat", nil)
	writeSSE(map[string]bool{"done": true})
}

// GET /api/chat/history → 最近 50 条（含限额余量）
func (s *Server) chatHistory(c *gin.Context) {
	kidID := kidOf(c)
	list, err := s.DB.ChatHistory(kidID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": list, "left_today": CHAT_DAILY_LIMIT - s.DB.ChatCountToday(kidID)})
}

var _ = io.Discard
