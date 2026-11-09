package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"meowquest/backend/internal/store"
)

// 猫砂盆状态：只存 0-3 的档位，不存每一砣结团的坐标（覆层里的砂块位置是固定预设点）。
// 变脏只能由猫「在线真用过一次」触发，绝不按时间或离线累积，避免变成每日必做的作业。
const (
	litterItemKey  = "litter-box"
	litterMaxState = 3
)

// 用 SQL 原子增减而非前端写绝对值：多标签页/多设备同时铲也不会互相覆盖。
// 表达式兼容 MySQL 与 PostgreSQL（两个驱动都在用）。
func litterUseExpr() clause.Expr {
	return gorm.Expr("LEAST(?, state + 1)", litterMaxState)
}

func litterScoopExpr(delta int) clause.Expr {
	return gorm.Expr("GREATEST(0, state - ?)", delta)
}

// 读回兜底：历史数据或外部写入越界时，前端小图仍能落在四档里。
func clampLitterState(n int) int {
	if n < 0 {
		return 0
	}
	if n > litterMaxState {
		return litterMaxState
	}
	return n
}

// loadLitterItem 只认「本人摆放的猫砂盆」，防止拿别的道具 id 或别人的行来改状态。
func (s *Server) loadLitterItem(c *gin.Context, id int64) (store.SceneItem, error) {
	var it store.SceneItem
	err := s.DB.Select("id", "state").
		Where("id=? AND kid_id=? AND item_key=?", id, kidOf(c), litterItemKey).
		First(&it).Error
	return it, err
}

func (s *Server) readLitterState(id, kidID int64) (int, error) {
	var it store.SceneItem
	if err := s.DB.Select("state").Where("id=? AND kid_id=?", id, kidID).First(&it).Error; err != nil {
		return 0, err
	}
	return clampLitterState(it.State), nil
}

// POST /api/litter/use { id } —— 猫走完「用砂盆」动画后才调，封顶 3。
func (s *Server) litterUse(c *gin.Context) {
	var req struct {
		ID int64 `json:"id" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := s.loadLitterItem(c, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "litter box not placed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 不用 RowsAffected 判断成功：state 已到 3 时 LEAST 不改动行，MySQL 可能返回 0。
	if err := s.DB.Model(&store.SceneItem{}).
		Where("id=? AND kid_id=? AND item_key=?", req.ID, kidOf(c), litterItemKey).
		Update("state", litterUseExpr()).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	state, err := s.readLitterState(req.ID, kidOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "state": state})
}

// POST /api/litter/scoop { id, delta } —— 关闭覆层时按实际铲掉数一次扣完，钳到 0。
func (s *Server) litterScoop(c *gin.Context) {
	var req struct {
		ID    int64 `json:"id" binding:"required,gt=0"`
		Delta int   `json:"delta" binding:"required,gt=0,lte=3"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := s.loadLitterItem(c, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "litter box not placed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&store.SceneItem{}).
		Where("id=? AND kid_id=? AND item_key=?", req.ID, kidOf(c), litterItemKey).
		Update("state", litterScoopExpr(req.Delta)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	state, err := s.readLitterState(req.ID, kidOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 铲过是真实经历，写进聊天上下文让猫能自然提起；eventCNSafe 已补中文映射。
	_ = s.DB.AddEvent(kidOf(c), "litter_scoop", map[string]any{"delta": req.Delta})
	c.JSON(http.StatusOK, gin.H{"ok": true, "state": state})
}
