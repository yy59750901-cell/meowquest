package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
	"meowquest/backend/internal/web"
)

type Server struct {
	DB  *store.DB
	PIN string // 兼容：默认家庭 PIN（存量）
	LLM *llm.Client
}

func New(db *store.DB, pin string) *Server {
	return &Server{DB: db, PIN: pin, LLM: llm.NewFromEnv()}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()
	api := r.Group("/api")
	{
		api.GET("/species", s.speciesList)

		// 孩子端进入流：输家庭码 → 列孩子/新建领养
		api.POST("/enter", s.enter) // {code} → {family, kids}
		api.POST("/adopt", s.adopt) // {code, kid_id?/kid_name, species, name}

		api.GET("/pet", s.kid, s.pet)
		api.POST("/pet/feed", s.kid, s.feed)
		api.POST("/pet/touch", s.kid, s.touch)
		api.GET("/pet/events", s.kid, s.events)
		api.GET("/tasks/today", s.kid, s.todayTasks)
		api.POST("/tasks/:id/submit", s.kid, s.submitTask)
		api.POST("/upload", s.kid, s.uploadEvidence)
		api.GET("/media/thumbnail", s.mediaThumbnail)
		api.HEAD("/media/thumbnail", s.mediaThumbnail)
		// 愿望
		api.POST("/wish", s.kid, s.createWish)
		api.GET("/wish/list", s.kid, s.wishList)
		api.GET("/wish/stars", s.kid, s.wishStars)
		api.POST("/wish/:id/redeem", s.kid, s.redeemWish)
		api.POST("/wish/:id/cancel", s.kid, s.cancelWish)
		// 猫聊天（SSE 流式）+ 语音识别
		api.POST("/chat", s.kid, s.chat)
		api.POST("/asr", s.kid, s.asr)
		api.GET("/chat/history", s.kid, s.chatHistory)

		// 商店与场景
		api.GET("/shop", s.kid, s.shopList)
		api.POST("/shop/buy", s.kid, s.buyItem)
		api.GET("/scene", s.kid, s.sceneLayout)
		api.POST("/scene/place", s.kid, s.placeItem)
		api.POST("/scene/remove", s.kid, s.removeItem)
	}
	admin := r.Group("/api/admin")
	{
		admin.POST("/login", s.adminLogin)      // {code, pin} → 家庭级 token
		admin.POST("/family", s.createFamily)   // 注册新家庭（产品化入口）
		admin.GET("/pet", s.admin, s.pet)       // 兼容旧接口（无 kid 维度则 404）
		admin.GET("/kids", s.admin, s.kidsList) // 家庭孩子列表
		admin.POST("/kid", s.admin, s.createKidAdmin)
		admin.POST("/task", s.admin, s.createTask)
		admin.PUT("/task/:id", s.admin, s.updateTask)
		admin.GET("/task", s.admin, s.listTasks)
		admin.POST("/task/:id/assign", s.admin, s.assignTask) // 指派给孩子
		admin.POST("/reward_policy", s.admin, s.upsertPolicy)
		admin.POST("/leave", s.admin, s.setLeave)
		admin.POST("/materialize", s.admin, s.rematerialize)
		admin.GET("/confirm/list", s.admin, s.confirmList)
		admin.GET("/confirm/history", s.admin, s.confirmHistory)
		admin.POST("/confirm/:id/ai-retry", s.admin, s.retryAIReview)
		admin.POST("/confirm/:id", s.admin, s.confirmTask)
		admin.GET("/ledger", s.admin, s.ledger)
		admin.GET("/ledger/:kid_id", s.admin, s.ledgerKid)
		admin.GET("/wish/list", s.admin, s.adminWishList)
		admin.POST("/wish/:id", s.admin, s.adminWishAction)
	}
	r.Static("/uploads", uploadDir())
	if h, err := web.Handler(); err == nil {
		r.NoRoute(gin.WrapH(h))
	} else {
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})
	}
	return r
}

// ===== 认证中间件 =====

// kid：孩子设备 token → kidID
func (s *Server) kid(c *gin.Context) {
	token, _ := c.Cookie("kid_token")
	if token == "" {
		token = c.GetHeader("X-Kid-Token")
	}
	kidID := s.DB.KidOfToken(token)
	if kidID == 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no kid, adopt first"})
		return
	}
	c.Set("kidID", kidID)
	c.Next()
}

// admin：家长 token → familyID（验签 + 家庭必须真实存在：换库/删家庭后旧 token 立即失效）
func (s *Server) admin(c *gin.Context) {
	token, _ := c.Cookie("admin_token")
	if token == "" {
		token = c.GetHeader("X-Admin-Token")
	}
	fid := store.FamilyOfAdminToken(token)
	if fid == 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "pin required"})
		return
	}
	var fam store.Family
	if err := s.DB.First(&fam, fid).Error; err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "family gone"})
		return
	}
	c.Set("familyID", fid)
	c.Next()
}

func kidOf(c *gin.Context) int64 { return c.GetInt64("kidID") }
func famOf(c *gin.Context) int64 { return c.GetInt64("familyID") }

// ===== 家长登录 / 注册 =====

// POST /api/admin/login {code, pin}
func (s *Server) adminLogin(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
		PIN  string `json:"pin" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fam, err := s.DB.FamilyLogin(req.Code, req.PIN)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "家庭码或 PIN 不对"})
		return
	}
	token := store.NewAdminToken(fam.ID)
	c.SetCookie("admin_token", token, 24*3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "token": token, "family": fam.Name, "family_id": fam.ID})
}

// POST /api/admin/family {name, pin} → 注册新家庭（返回家庭码）
func (s *Server) createFamily(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		PIN  string `json:"pin" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fam, err := s.DB.CreateFamily(req.Name, req.PIN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	token := store.NewAdminToken(fam.ID)
	c.JSON(http.StatusOK, gin.H{"family": fam, "code": fam.Code, "token": token})
}

// GET /api/admin/kids 家庭孩子列表
func (s *Server) kidsList(c *gin.Context) {
	kids, err := s.DB.KidsOfFamily(famOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"kids": kids})
}

// POST /api/admin/kid {name}
func (s *Server) createKidAdmin(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kid, err := s.DB.CreateKid(famOf(c), req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"kid": kid})
}

// ===== 孩子端进入流 =====

// POST /api/enter {code} → {family, kids}
func (s *Server) enter(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fam, err := s.DB.FamilyByCode(req.Code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "家庭码不存在，问问爸爸妈妈吧"})
		return
	}
	kids, _ := s.DB.KidsOfFamily(fam.ID)
	type kidInfo struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		HasPet bool   `json:"has_pet"`
	}
	var out []kidInfo
	for _, k := range kids {
		has, _ := s.DB.HasPet(k.ID)
		out = append(out, kidInfo{ID: k.ID, Name: k.Name, HasPet: has})
	}
	c.JSON(http.StatusOK, gin.H{"family": fam.Name, "kids": out})
}

// POST /api/adopt {code, kid_id?, kid_name?, species, name, personality?}
// kid_id=选择已有孩子（换设备/多孩同设备）；无 kid_id=新建孩子并领养
func (s *Server) adopt(c *gin.Context) {
	var req struct {
		Code        string `json:"code" binding:"required"`
		KidID       int64  `json:"kid_id"`
		KidName     string `json:"kid_name"`
		Species     string `json:"species"` // 新建分支内校验（选已有孩子时不需要）
		Name        string `json:"name"`
		Personality string `json:"personality"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fam, err := s.DB.FamilyByCode(req.Code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "家庭码不存在"})
		return
	}

	var kid store.Kid
	if req.KidID > 0 {
		if err := s.DB.Where("id=? AND family_id=?", req.KidID, fam.ID).First(&kid).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "孩子不存在"})
			return
		}
		// 还没猫的孩子：顺便完成领养（前端从选择页进领养流）
		if req.Species != "" && req.Name != "" {
			if _, err := s.DB.AdoptPet(kid.ID, req.Name, req.Species, defStr(req.Personality, "热情开朗")); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
		token := store.NewToken()
		if err := s.DB.SaveToken(token, kid.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.SetCookie("kid_token", token, 10*365*24*3600, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"adopted": true, "rebind": true, "kid": kid})
		return
	}

	// 新建孩子 + 领养（选已有孩子但没猫时 kid_id>0，上面已处理）
	if req.Species == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "species and name required"})
		return
	}
	kidName := req.KidName
	if kidName == "" {
		kidName = "宝贝"
	}
	newKid, err := s.DB.CreateKid(fam.ID, kidName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	personality := req.Personality
	if personality == "" {
		personality = "热情开朗"
	}
	if _, err := s.DB.AdoptPet(newKid.ID, req.Name, req.Species, personality); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	token := store.NewToken()
	if err := s.DB.SaveToken(token, newKid.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.SetCookie("kid_token", token, 10*365*24*3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"adopted": true, "kid": newKid})
}

// ===== 宠物（per-kid） =====

func (s *Server) pet(c *gin.Context) {
	p, err := s.DB.GetPet(kidOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no pet"})
		return
	}
	var kid store.Kid
	s.DB.First(&kid, p.KidID)
	c.JSON(http.StatusOK, gin.H{"pet": p, "kid": kid})
}

func (s *Server) feed(c *gin.Context) {
	if err := s.DB.FeedPet(kidOf(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.pet(c)
}

func (s *Server) touch(c *gin.Context) {
	left, err := s.DB.TouchPet(kidOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"touch_left_today": left})
}

func (s *Server) events(c *gin.Context) {
	day := c.Query("since")
	if day == "" {
		day = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	list, err := s.DB.EventsSince(kidOf(c), day)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": list})
}

func (s *Server) speciesList(c *gin.Context) {
	list, err := s.DB.SpeciesList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"species": list})
}
