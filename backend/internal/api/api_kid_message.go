package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"meowquest/backend/internal/store"
)

// ===== 猫的信箱：家长审核后才给孩子留言，孩子端可回看，不受"只看今天"限制 =====

const (
	kidMessageApproved = "approved"
	kidMessageRejected = "rejected"
)

// kidApprovedContent 通过时优先用 AI 给孩子的话；AI 没给就退回中性文案。
func kidApprovedContent(v reviewVerdict) string {
	if text := strings.TrimSpace(v.KidFeedback); text != "" {
		return clipRunes(text, 120)
	}
	if !v.Pass {
		return "猫看完啦，妈妈说这次也算完成，下次再讲得更完整一点会更好喵～"
	}
	return "猫看完啦，妈妈说你做得很棒！"
}

// kidRejectedContent 驳回用固定模板，不再调 LLM：省一次调用，也不会出现
// "AI 夸得很好、家长却驳回"的自相矛盾文案。
func kidRejectedContent(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "主人，妈妈想再看看这次的材料，补一点就好，不用重新计时喵～"
	}
	return "主人，妈妈想再看看" + clipRunes(reason, 30) + "，补一点就好，不用重新计时喵～"
}

// kidMessageScoreMax 与提示词里的 1-5 分契约对齐；模型偶尔给越界值，入库前夹一下。
const kidMessageScoreMax = 5

// kidMessageScore 夹取评分：驳回或越界统一为 0（前端按"无评分"展示）。
func kidMessageScore(kind string, score int) int {
	if kind != kidMessageApproved || score < 1 {
		return 0
	}
	if score > kidMessageScoreMax {
		return kidMessageScoreMax
	}
	return score
}

// writeKidMessage 与审核同一个事务写入，避免"审核成功但留言丢了"。
func writeKidMessage(tx *gorm.DB, kidID, completionID int64, kind, taskName, content string, score int, highlights []string) error {
	return tx.Create(&store.KidMessage{KidID: kidID, CompletionID: completionID, Kind: kind,
		TaskName: clipRunes(taskName, 32), Score: kidMessageScore(kind, score),
		Content: content, Highlights: highlights}).Error
}

// snapshotName 取任务名用于留言标题；快照损坏时退回空串，不影响审核。
func snapshotName(snapshot string) string {
	var snap struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(snapshot), &snap); err != nil {
		return ""
	}
	return snap.Name
}

// verdictOf 解析已落库的 AI 评价；没有或解析不出就当没有评价处理，不影响家长审核结果。
func verdictOf(verdict string) reviewVerdict {
	var v reviewVerdict
	if verdict == "" {
		return v
	}
	if err := json.Unmarshal([]byte(verdict), &v); err != nil {
		return reviewVerdict{}
	}
	return v
}

// GET /api/kid/messages 猫的信箱：最近的留言 + 未读条数。
func (s *Server) kidMessages(c *gin.Context) {
	kidID := kidOf(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	var items []store.KidMessage
	if err := s.DB.Where("kid_id=?", kidID).Order("id DESC").Limit(limit).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var unread int64
	if err := s.DB.Model(&store.KidMessage{}).Where("kid_id=? AND read_at IS NULL", kidID).Count(&unread).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "unread": unread})
}

// POST /api/kid/messages/read 标记已读；不带 ids 表示全部已读。
func (s *Server) readKidMessages(c *gin.Context) {
	kidID := kidOf(c)
	var req struct {
		IDs []int64 `json:"ids"`
	}
	// 空 body 合法（全部已读），只忽略解析错误。
	_ = c.ShouldBindJSON(&req)
	now := time.Now()
	scope := s.DB.Model(&store.KidMessage{}).Where("kid_id=? AND read_at IS NULL", kidID)
	if len(req.IDs) > 0 {
		scope = scope.Where("id IN ?", req.IDs)
	}
	if err := scope.Update("read_at", &now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var unread int64
	if err := s.DB.Model(&store.KidMessage{}).Where("kid_id=? AND read_at IS NULL", kidID).Count(&unread).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread": unread})
}
