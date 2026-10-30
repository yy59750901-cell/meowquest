package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

// GET /api/admin/confirm/history 每个已处理 completion 独立保留，不随任务重做覆盖。
func (s *Server) confirmHistory(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page 无效"})
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || size < 1 || size > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_size 应为 1 至 100"})
		return
	}
	items := []confirmItem{}
	var total int64
	err = s.DB.Table("task_completion p").Joins("JOIN daily_task d ON d.id=p.daily_task_id").
		Joins("JOIN kid k ON k.id=d.kid_id").Where("k.family_id=? AND p.confirm_status<>'pending'", famOf(c)).Count(&total).Error
	if err == nil {
		err = s.DB.Raw(confirmSelect+`WHERE k.family_id=? AND p.confirm_status<>'pending'
			ORDER BY p.submitted_at DESC,p.id DESC LIMIT ? OFFSET ?`, famOf(c), size, (page-1)*size).Scan(&items).Error
	}
	if err == nil {
		err = attachReviewViews(s.DB.DB, items)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

// POST /api/admin/confirm/:id/ai-retry 此处 id 明确是 completion_id，不是 daily_task_id。
func (s *Server) retryAIReview(c *gin.Context) {
	id := atoi64(c.Param("id"))
	status := http.StatusInternalServerError
	var job store.AIReviewJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var comp store.TaskCompletion
		if err := tx.Where("id=? AND kid_id IN (SELECT id FROM kid WHERE family_id=?)", id, famOf(c)).First(&comp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				status = http.StatusNotFound
			}
			return err
		}
		var dt store.DailyTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND kid_id=? AND kid_id IN (SELECT id FROM kid WHERE family_id=?)", comp.DailyTaskID, comp.KidID, famOf(c)).First(&dt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				status = http.StatusNotFound
			}
			return err
		}
		// 必须锁后重新读取，人工确认或新提交可能发生在取得任务锁之前。
		var latest store.TaskCompletion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("daily_task_id=?", dt.ID).Order("id DESC").First(&latest).Error; err != nil {
			return err
		}
		if latest.ID != id || latest.ConfirmStatus != "pending" || dt.Status != "submitted" {
			status = http.StatusConflict
			return fmt.Errorf("只能重试当前未处理的最新提交")
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("completion_id=?", id).First(&job).Error
		if err == nil {
			switch job.Status {
			case "queued", "running":
				return nil
			case "failed":
				job.Status, job.Stage, job.Error, job.Result = "queued", "queued", "", ""
				job.StartedAt, job.FinishedAt, job.LeaseExpiresAt, job.LeaseToken = nil, nil, nil, ""
				return tx.Model(&store.AIReviewJob{}).Where("id=? AND status='failed'", job.ID).Updates(map[string]any{
					"status": "queued", "stage": "queued", "error": "", "result": "",
					"started_at": nil, "finished_at": nil, "lease_token": "", "lease_expires_at": nil,
				}).Error
			default:
				status = http.StatusConflict
				return fmt.Errorf("AI 任务不是可重试的失败状态")
			}
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var snap engine.Snapshot
		if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
			return err
		}
		mode := snap.ConfirmMode
		if mode == "" {
			var tpl store.TaskTemplate
			if err := tx.First(&tpl, dt.TemplateID).Error; err != nil {
				return err
			}
			mode = tpl.ConfirmMode
		}
		if latest.ReviewResult != "" || !confirmModeContains(mode, 'b') {
			status = http.StatusConflict
			return fmt.Errorf("该提交不需要 AI 重试")
		}
		job, err = newReviewJob(tx, dt, latest)
		if err != nil {
			return err
		}
		return tx.Create(&job).Error
	})
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"completion_id": id, "ai_queued": true, "ai_review": reviewView(job)})
}
