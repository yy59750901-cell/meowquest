package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/store"
	"meowquest/backend/internal/web"
)

type Server struct {
	DB  *store.DB
	PIN string // 家长端 PIN（配置文件/环境变量）
}

func New(db *store.DB, pin string) *Server {
	return &Server{DB: db, PIN: pin}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()
	api := r.Group("/api")
	{
		// 孩子端（免登录 token）
		api.GET("/species", s.speciesList)   // 可领养品种（后台配置）
		api.POST("/adopt", s.adopt)          // 领养（签发 token）
		api.GET("/pet", s.kid, s.pet)        // 宠物实时状态
		api.POST("/pet/feed", s.kid, s.feed) // 喂食
		api.POST("/pet/touch", s.kid, s.touch)
		api.GET("/pet/events", s.kid, s.events) // 事件流（日记素材）
		api.GET("/tasks/today", s.kid, s.todayTasks)
		api.POST("/tasks/:id/submit", s.kid, s.submitTask)
		// 愿望
		api.POST("/wish", s.kid, s.createWish)
		api.GET("/wish/list", s.kid, s.wishList)
		api.GET("/wish/stars", s.kid, s.wishStars)
		api.POST("/wish/:id/redeem", s.kid, s.redeemWish)
		api.POST("/wish/:id/cancel", s.kid, s.cancelWish)
		// 商店与场景
		api.GET("/shop", s.kid, s.shopList)
		api.POST("/shop/buy", s.kid, s.buyItem)
		api.POST("/upload", s.kid, s.uploadEvidence) // 任务证据上传
		api.GET("/scene", s.kid, s.sceneLayout)
		api.POST("/scene/place", s.kid, s.placeItem)
		api.POST("/scene/remove", s.kid, s.removeItem)
	}
	admin := r.Group("/api/admin")
	{
		admin.POST("/login", s.adminLogin)
		admin.GET("/pet", s.admin, s.pet) // 家长也能看猫
		admin.POST("/task", s.admin, s.createTask)      // 创建任务（模板+调度）
		admin.PUT("/task/:id", s.admin, s.updateTask)   // 修改
		admin.GET("/task", s.admin, s.listTasks)        // 列表
		admin.POST("/reward_policy", s.admin, s.upsertPolicy) // 倍数策略
		admin.POST("/leave", s.admin, s.setLeave)       // 请假
		admin.POST("/materialize", s.admin, s.rematerialize) // 手动物化（调试）
		admin.GET("/confirm/list", s.admin, s.confirmList)   // 待确认队列
		admin.POST("/confirm/:id", s.admin, s.confirmTask)   // 通过/驳回
		admin.GET("/ledger", s.admin, s.ledger)              // 星星流水
		admin.GET("/wish/list", s.admin, s.adminWishList)    // 愿望审批列表
		admin.POST("/wish/:id", s.admin, s.adminWishAction)  // 定价/驳回/兑现
	}
	// 上传的证据文件（本地磁盘）
	r.Static("/uploads", uploadDir())

	// 前端产物（embed 单二进制）
	if h, err := web.Handler(); err == nil {
		r.NoRoute(gin.WrapH(h))
	} else {
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})
	}
	return r
}

// ===== 孩子端认证中间件 =====
func (s *Server) kid(c *gin.Context) {
	token, _ := c.Cookie("kid_token")
	if token == "" {
		token = c.GetHeader("X-Kid-Token")
	}
	if !s.DB.ValidToken(token) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no pet, adopt first"})
		return
	}
	c.Next()
}

// ===== 家长端认证 =====
func (s *Server) admin(c *gin.Context) {
	token, _ := c.Cookie("admin_token")
	if token == "" {
		token = c.GetHeader("X-Admin-Token") // 手机浏览器 cookie 兼容兜底
	}
	if !store.AdminLogin(token) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "pin required"})
		return
	}
	c.Next()
}

func (s *Server) adminLogin(c *gin.Context) {
	var req struct {
		PIN string `json:"pin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.PIN != s.PIN {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "wrong pin"})
		return
	}
	token := store.NewAdminToken()
	c.SetCookie("admin_token", token, 24*3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "token": token})
}

// ===== 业务接口 =====
func (s *Server) speciesList(c *gin.Context) {
	list, err := s.DB.SpeciesList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"species": list})
}

func (s *Server) adopt(c *gin.Context) {
	// 已有 token 且已领养 → 直接返回
	if token, _ := c.Cookie("kid_token"); s.DB.ValidToken(token) {
		if ok, _ := s.DB.HasPet(); ok {
			c.JSON(http.StatusOK, gin.H{"adopted": true})
			return
		}
	}
	var req struct {
		Species     string `json:"species" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Personality string `json:"personality"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ok, _ := s.DB.HasPet(); ok {
		// 已被领养但本设备无 token：换设备场景，签发新 token 绑定同一只猫
		token := store.NewToken()
		if err := s.DB.SaveToken(token); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.SetCookie("kid_token", token, 365*24*3600, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"adopted": true, "rebind": true})
		return
	}
	personality := req.Personality
	if personality == "" {
		personality = "热情开朗"
	}
	if _, err := s.DB.AdoptPet(req.Name, req.Species, personality); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	token := store.NewToken()
	if err := s.DB.SaveToken(token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.SetCookie("kid_token", token, 365*24*3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"adopted": true})
}

func (s *Server) pet(c *gin.Context) {
	p, err := s.DB.GetPet()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no pet"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pet": p})
}

func (s *Server) feed(c *gin.Context) {
	if err := s.DB.FeedPet(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.pet(c)
}

func (s *Server) touch(c *gin.Context) {
	left, err := s.DB.TouchPet()
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
	list, err := s.DB.EventsSince(day)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": list})
}
