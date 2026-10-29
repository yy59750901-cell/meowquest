package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"meowquest/backend/internal/store"
)

func now() time.Time { return time.Now() }

type wishAdminItem struct {
	ID           int64     `json:"id"`
	KidID        int64     `json:"kid_id"`
	KidName      string    `json:"kid_name"`
	Title        string    `json:"title"`
	Photo        string    `json:"photo"`
	Status       string    `json:"status"`
	Price        int       `json:"price"`
	HopedPrice   int       `json:"hoped_price"`
	RejectReason string    `json:"reject_reason"`
	CreatedAt    time.Time `json:"created_at"`
}

// ===== 愿望系统 =====

// POST /api/wish 孩子许愿
func (s *Server) createWish(c *gin.Context) {
	var req struct {
		Title      string `json:"title" binding:"required"`
		Photo      string `json:"photo"`
		HopedPrice int    `json:"hoped_price"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var active int64
	s.DB.Model(&store.Wish{}).Where("kid_id=? AND status NOT IN ('fulfilled','rejected')", kidOf(c)).Count(&active)
	if active >= 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "最多同时许 3 个愿望哦，先等等吧"})
		return
	}
	w := store.Wish{KidID: kidOf(c), Title: req.Title, Photo: req.Photo, HopedPrice: req.HopedPrice}
	if err := s.DB.Create(&w).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.DB.AddEvent(kidOf(c), "wish_created", map[string]any{"title": req.Title})
	c.JSON(http.StatusOK, gin.H{"id": w.ID, "status": w.Status})
}

// GET /api/wish/list 孩子端：愿望列表 + 进度
func (s *Server) wishList(c *gin.Context) {
	var wishes []store.Wish
	if err := s.DB.Where("kid_id=? AND status<>'rejected'", kidOf(c)).Order("id DESC").Find(&wishes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pet, _ := s.DB.GetPet(kidOf(c))
	var balance int64
	if pet != nil {
		balance = pet.Balance
	}
	out := make([]gin.H, 0, len(wishes))
	for _, w := range wishes {
		item := gin.H{"id": w.ID, "title": w.Title, "photo": w.Photo, "status": w.Status,
			"price": w.Price, "hoped_price": w.HopedPrice, "created_at": w.CreatedAt}
		if w.Status == "saving" && w.Price > 0 {
			progress := 0
			if balance >= int64(w.Price) {
				progress = 100
			} else {
				progress = int(float64(balance) / float64(w.Price) * 100)
			}
			item["progress"] = progress
			item["balance"] = balance
			item["can_redeem"] = balance >= int64(w.Price)
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"wishes": out, "balance": balance})
}

// POST /api/wish/:id/redeem 孩子兑换：冻结扣星
func (s *Server) redeemWish(c *gin.Context) {
	var w store.Wish
	if err := s.DB.First(&w, "id=?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if w.Status != "approved" && w.Status != "saving" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wish status " + w.Status})
		return
	}
	if err := s.DB.SpendStars(kidOf(c), w.Price); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "星星还不够，继续加油喵！"})
		return
	}
	s.DB.Model(&w).Updates(map[string]any{"status": "redeem_requested", "redeemed_at": now()})
	s.DB.AddEvent(kidOf(c), "wish_redeem", map[string]any{"wish_id": w.ID, "price": w.Price})
	c.JSON(http.StatusOK, gin.H{"redeem_requested": true})
}

// POST /api/wish/:id/cancel 孩子撤回
func (s *Server) cancelWish(c *gin.Context) {
	s.DB.Model(&store.Wish{}).Where("id=? AND status IN ('pending','saving')", c.Param("id")).
		Updates(map[string]any{"status": "rejected", "reject_reason": "我自己不想许这个愿啦"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/wish/stars 愿望星图（已兑现墙）
func (s *Server) wishStars(c *gin.Context) {
	var wishes []store.Wish
	if err := s.DB.Where("kid_id=? AND status='fulfilled'", kidOf(c)).Order("fulfilled_at DESC").Find(&wishes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stars": wishes})
}

// ===== 家长端：愿望审批 =====

// GET /api/admin/wish/list 全部愿望 + 定价健康度
func (s *Server) adminWishList(c *gin.Context) {
	var wishes []wishAdminItem
	if err := s.DB.Raw(`SELECT w.id, w.kid_id, COALESCE(k.name,'') AS kid_name, w.title, COALESCE(w.photo,'') AS photo,
		w.status, w.price, w.hoped_price, COALESCE(w.reject_reason,'') AS reject_reason, w.created_at
		FROM wish w JOIN kid k ON k.id=w.kid_id
		WHERE k.family_id=? ORDER BY w.id DESC`, famOf(c)).Scan(&wishes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 近30天日均星星（笔均值 × 日均笔数）
	var avg, perDay float64
	s.DB.Raw(`SELECT COALESCE(AVG(final),0) FROM settlement_ledger
		WHERE date>=DATE_SUB(CURDATE(), INTERVAL 30 DAY) AND final>0`).Scan(&avg)
	s.DB.Raw(`SELECT COALESCE(AVG(n),0) FROM (SELECT COUNT(*) n FROM settlement_ledger
		WHERE date>=DATE_SUB(CURDATE(), INTERVAL 30 DAY) GROUP BY date) t`).Scan(&perDay)
	healthBase := avg * perDay

	out := make([]gin.H, 0, len(wishes))
	for _, w := range wishes {
		item := gin.H{"id": w.ID, "title": w.Title, "photo": w.Photo, "status": w.Status,
			"price": w.Price, "hoped_price": w.HopedPrice, "reject_reason": w.RejectReason,
			"created_at": w.CreatedAt}
		if healthBase > 0 {
			ref := w.Price
			if ref == 0 {
				ref = w.HopedPrice
			}
			if ref > 0 {
				item["suggest_days"] = int(float64(ref)/healthBase + 0.5)
			}
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"wishes": out, "daily_stars_avg": healthBase})
}

// POST /api/admin/wish/:id { action: approve/reject/fulfill, price, reason }
func (s *Server) adminWishAction(c *gin.Context) {
	var req struct {
		Action string `json:"action" binding:"required"`
		Price  int    `json:"price"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var w store.Wish
	if err := s.DB.First(&w, "id=?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	switch req.Action {
	case "approve":
		if req.Price <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price required"})
			return
		}
		if err := s.DB.Model(&w).Where("status='pending'").
			Updates(map[string]any{"status": "saving", "price": req.Price}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	case "reject":
		s.DB.Model(&w).Where("status IN ('pending','saving')").
			Updates(map[string]any{"status": "rejected", "reject_reason": req.Reason})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	case "fulfill":
		s.DB.Model(&w).Where("status='redeem_requested'").
			Updates(map[string]any{"status": "fulfilled", "fulfilled_at": now()})
		s.DB.AddEvent(w.KidID, "wish_fulfilled", map[string]any{"wish_id": w.ID})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown action"})
	}
}

// ===== 道具商店（插件化注册表） =====

var ITEM_REGISTRY = []gin.H{
	{"key": "nest", "name": "猫窝", "price": 30, "type": "furniture"},
	{"key": "yarn", "name": "毛线球", "price": 15, "type": "furniture"},
	{"key": "plant", "name": "盆栽", "price": 20, "type": "furniture"},
	{"key": "fish", "name": "小鱼挂画", "price": 12, "type": "furniture"},
	{"key": "wp-pink", "name": "粉色壁纸", "price": 25, "type": "wallpaper"},
	{"key": "wp-blue", "name": "蓝色壁纸", "price": 25, "type": "wallpaper"},
	{"key": "wp-mint", "name": "薄荷壁纸", "price": 25, "type": "wallpaper"},
}

// GET /api/shop 注册表 + 已购
func (s *Server) shopList(c *gin.Context) {
	var owned []store.OwnedItem
	s.DB.Where("kid_id=?", kidOf(c)).Find(&owned)
	ownedMap := map[string]bool{}
	for _, o := range owned {
		ownedMap[o.ItemKey] = true
	}
	items := make([]gin.H, len(ITEM_REGISTRY))
	for i, it := range ITEM_REGISTRY {
		m := gin.H{}
		for k, v := range it {
			m[k] = v
		}
		m["owned"] = ownedMap[it["key"].(string)]
		items[i] = m
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// POST /api/shop/buy { key }
func (s *Server) buyItem(c *gin.Context) {
	var req struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var price int
	for _, it := range ITEM_REGISTRY {
		if it["key"] == req.Key {
			price = it["price"].(int)
		}
	}
	if price == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}
	var exists int64
	s.DB.Model(&store.OwnedItem{}).Where("kid_id=? AND item_key=?", kidOf(c), req.Key).Count(&exists)
	if exists > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already owned"})
		return
	}
	// 事务：扣星 + 入货原子（失败整体回滚，杜绝扣钱不给货）
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.Pet{}).Where("balance>=?", price).
			Update("balance", gorm.Expr("balance-?", price))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("insufficient stars")
		}
		return tx.Create(&store.OwnedItem{KidID: kidOf(c), ItemKey: req.Key}).Error
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "星星不够，先做任务！"})
		return
	}
	s.DB.AddEvent(kidOf(c), "item_bought", map[string]any{"key": req.Key, "price": price})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/scene 场景布局
func (s *Server) sceneLayout(c *gin.Context) {
	var items []store.SceneItem
	if err := s.DB.Where("kid_id=?", kidOf(c)).Order("placed_at").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// POST /api/scene/place { id?, key, x, y }
func (s *Server) placeItem(c *gin.Context) {
	var req struct {
		ID  int64   `json:"id"`
		Key string  `json:"key" binding:"required"`
		X   float64 `json:"x" binding:"required"`
		Y   float64 `json:"y" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var owned int64
	s.DB.Model(&store.OwnedItem{}).Where("kid_id=? AND item_key=?", kidOf(c), req.Key).Count(&owned)
	if owned == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not owned"})
		return
	}
	if req.ID > 0 {
		s.DB.Model(&store.SceneItem{}).Where("id=? AND item_key=?", req.ID, req.Key).
			Updates(map[string]any{"x": req.X, "y": req.Y})
	} else {
		s.DB.Create(&store.SceneItem{KidID: kidOf(c), ItemKey: req.Key, X: req.X, Y: req.Y})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/scene/remove { id }
func (s *Server) removeItem(c *gin.Context) {
	var req struct {
		ID int64 `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.DB.Delete(&store.SceneItem{}, req.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
