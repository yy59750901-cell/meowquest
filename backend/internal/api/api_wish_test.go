package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/store"
)

func TestItemRegistry(t *testing.T) {
	seen := make(map[string]bool, len(ITEM_REGISTRY))
	foundCatTree := false
	for i, item := range ITEM_REGISTRY {
		key, ok := item["key"].(string)
		if !ok || key == "" {
			t.Fatalf("第 %d 个商品的 key 必须是非空字符串", i)
		}
		if seen[key] {
			t.Errorf("商品 key 重复: %q", key)
		}
		seen[key] = true
		price, ok := item["price"].(int)
		if !ok || price <= 0 {
			t.Errorf("商品 %q 的价格必须是正整数，实际为 %v", key, item["price"])
		}
		if key == "cat-tree" {
			foundCatTree = true
			if item["name"] != "猫爬架" || price != 50 || item["type"] != "furniture" {
				t.Errorf("猫爬架必须为家具且售价 50 星星，实际为 %v", item)
			}
		}
	}
	if !foundCatTree {
		t.Fatal("商品注册表缺少 cat-tree")
	}
}

func sceneTestRouter(s *Server, kid store.Kid) *gin.Engine {
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	r.POST("/api/scene/place", s.placeItem)
	r.POST("/api/scene/remove", s.removeItem)
	return r
}

func sceneIntegrationFixture(t *testing.T) (*Server, store.Kid, store.SceneItem) {
	t.Helper()
	db := taskIntegrationDB(t)
	// 仅在现有测试设施创建的隔离 schema 中建场景表，不修改生产 schema。
	if err := db.AutoMigrate(&store.SceneItem{}); err != nil {
		t.Fatal(err)
	}
	fam := store.Family{Code: "445566", Name: "场景回归测试"}
	mustTaskCreate(t, db, &fam)
	kid := store.Kid{FamilyID: fam.ID, Name: "摆放孩子"}
	mustTaskCreate(t, db, &kid)
	mustTaskCreate(t, db, &store.OwnedItem{KidID: kid.ID, ItemKey: "cat-tree"})
	item := store.SceneItem{KidID: kid.ID, ItemKey: "cat-tree", X: 100, Y: 500}
	mustTaskCreate(t, db, &item)
	return &Server{DB: db}, kid, item
}

func TestCatTreePurchaseAndReuseIntegration(t *testing.T) {
	db := taskIntegrationDB(t)
	if err := db.AutoMigrate(&store.SceneItem{}); err != nil {
		t.Fatal(err)
	}
	fam := store.Family{Code: "223344", Name: "爬架购买回归"}
	mustTaskCreate(t, db, &fam)
	kid := store.Kid{FamilyID: fam.ID, Name: "购买孩子"}
	mustTaskCreate(t, db, &kid)
	pet := store.Pet{KidID: kid.ID, Name: "黑黑", Species: "black", Balance: 100, EarnedTotal: 100}
	mustTaskCreate(t, db, &pet)
	s := &Server{DB: db}
	r := sceneTestRouter(s, kid)
	r.GET("/api/shop", s.shopList)

	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"key": "cat-tree", "x": 400, "y": 600}), http.StatusBadRequest)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy",
		map[string]any{"key": "cat-tree", "price": 1}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy",
		map[string]any{"key": "cat-tree"}), http.StatusBadRequest)
	var current store.Pet
	if err := db.First(&current, pet.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Balance != 50 || current.EarnedTotal != 100 {
		t.Fatalf("仅按服务端50星星购买一次，不改变累计收益：%+v", current)
	}
	w := taskTestRequest(r, http.MethodGet, "/api/shop", nil)
	requireTaskStatus(t, w, http.StatusOK)
	var catalog struct {
		Items []struct {
			Key   string `json:"key"`
			Price int    `json:"price"`
			Owned bool   `json:"owned"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range catalog.Items {
		if item.Key == "cat-tree" {
			found = item.Owned && item.Price == 50
		}
	}
	if !found {
		t.Fatal("购买后商品必须显示已拥有且价格仍为50")
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"key": "cat-tree", "x": 400, "y": 600}), http.StatusOK)
	var scene store.SceneItem
	if err := db.Where("kid_id=? AND item_key=?", kid.ID, "cat-tree").First(&scene).Error; err != nil {
		t.Fatal(err)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/remove",
		map[string]any{"id": scene.ID}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"key": "cat-tree", "x": 500, "y": 620}), http.StatusOK)
	if err := db.First(&current, pet.ID).Error; err != nil || current.Balance != 50 {
		t.Fatalf("收回重摆不应再次扣星星：%+v, %v", current, err)
	}
}

func TestSceneItemOwnershipIntegration(t *testing.T) {
	s, kid, own := sceneIntegrationFixture(t)
	r := sceneTestRouter(s, kid)
	for _, crossFamily := range []bool{false, true} {
		familyID := kid.FamilyID
		if crossFamily {
			fam := store.Family{Code: "667788", Name: "另一家庭"}
			mustTaskCreate(t, s.DB, &fam)
			familyID = fam.ID
		}
		other := store.Kid{FamilyID: familyID, Name: "另一孩子"}
		mustTaskCreate(t, s.DB, &other)
		mustTaskCreate(t, s.DB, &store.OwnedItem{KidID: other.ID, ItemKey: "cat-tree"})
		foreign := store.SceneItem{KidID: other.ID, ItemKey: "cat-tree", X: 200, Y: 550}
		mustTaskCreate(t, s.DB, &foreign)
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
			map[string]any{"id": foreign.ID, "key": "cat-tree", "x": 300, "y": 600}), http.StatusNotFound)
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/remove",
			map[string]any{"id": foreign.ID}), http.StatusNotFound)
		var current store.SceneItem
		if err := s.DB.First(&current, foreign.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.ID != foreign.ID || current.KidID != foreign.KidID || current.ItemKey != foreign.ItemKey || current.X != foreign.X || current.Y != foreign.Y {
			t.Fatalf("跨孩子请求修改了场景：%+v", current)
		}
	}

	for _, id := range []int64{0, own.ID} {
		w := taskTestRequest(r, http.MethodPost, "/api/scene/place",
			map[string]any{"id": id, "key": "plant", "x": 300, "y": 600})
		requireTaskStatus(t, w, http.StatusBadRequest)
		if !strings.Contains(w.Body.String(), `"error":"not owned"`) {
			t.Fatalf("未购买错误契约变化：%s", w.Body)
		}
	}
	mustTaskCreate(t, s.DB, &store.OwnedItem{KidID: kid.ID, ItemKey: "nest"})
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"id": own.ID, "key": "nest", "x": 300, "y": 600}), http.StatusNotFound)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"id": int64(999999), "key": "cat-tree", "x": 300, "y": 600}), http.StatusNotFound)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/remove",
		map[string]any{"id": int64(999999)}), http.StatusNotFound)

	for _, point := range [][2]float64{{1, 1}, {1280, 720}, {1280, 720}} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
			map[string]any{"id": own.ID, "key": "cat-tree", "x": point[0], "y": point[1]}), http.StatusOK)
		var current store.SceneItem
		if err := s.DB.First(&current, own.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.KidID != kid.ID || current.ItemKey != own.ItemKey || current.X != point[0] || current.Y != point[1] {
			t.Fatalf("本孩子场景更新错误：%+v", current)
		}
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/remove",
		map[string]any{"id": own.ID}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/remove",
		map[string]any{"id": own.ID}), http.StatusNotFound)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/scene/place",
		map[string]any{"key": "cat-tree", "x": 400, "y": 600}), http.StatusOK)
	var items []store.SceneItem
	if err := s.DB.Where("kid_id=?", kid.ID).Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ItemKey != "cat-tree" || items[0].X != 400 || items[0].Y != 600 {
		t.Fatalf("重新摆放的归属或位置错误：%+v", items)
	}
	var owned int64
	if err := s.DB.Model(&store.OwnedItem{}).Where("kid_id=? AND item_key=?", kid.ID, "cat-tree").Count(&owned).Error; err != nil || owned != 1 {
		t.Fatalf("收回不应删除已购记录：%d，%v", owned, err)
	}
}

func TestSceneItemDatabaseErrorsIntegration(t *testing.T) {
	for _, operation := range []string{"权限查询", "新建", "更新", "删除"} {
		t.Run(operation, func(t *testing.T) {
			s, kid, item := sceneIntegrationFixture(t)
			table := "scene_item"
			if operation == "权限查询" {
				table = "owned_item"
			}
			// 真实 PG 错误；重命名只影响本用例的隔离 schema，无假数据库或回调桩。
			if err := s.DB.Exec("ALTER TABLE " + table + " RENAME TO unavailable_" + table).Error; err != nil {
				t.Fatal(err)
			}
			path := "/api/scene/place"
			body := map[string]any{"key": "cat-tree", "x": 300, "y": 600}
			if operation == "更新" || operation == "删除" {
				body["id"] = item.ID
			}
			if operation == "删除" {
				path = "/api/scene/remove"
			}
			w := taskTestRequest(sceneTestRouter(s, kid), http.MethodPost, path, body)
			requireTaskStatus(t, w, http.StatusInternalServerError)
			var response struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.OK || response.Error == "" {
				t.Fatalf("数据库失败返回了成功或丢失错误：%s，%v", w.Body, err)
			}
		})
	}
}

func TestSceneItemInvalidRequests(t *testing.T) {
	r := sceneTestRouter(&Server{}, store.Kid{ID: 1, FamilyID: 1})
	cases := []struct {
		path string
		body string
	}{
		{"/api/scene/remove", `{}`},
		{"/api/scene/remove", `{"id":null}`},
		{"/api/scene/remove", `{"id":0}`},
		{"/api/scene/remove", `{"id":-1}`},
		{"/api/scene/remove", `{"id":"1"}`},
		{"/api/scene/remove", `{"id":1.5}`},
		{"/api/scene/place", `{"key":"cat-tree","y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":100}`},
		{"/api/scene/place", `{"key":"cat-tree","x":null,"y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":0,"y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":100,"y":0}`},
		{"/api/scene/place", `{"key":"cat-tree","x":-1,"y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":100,"y":-1}`},
		{"/api/scene/place", `{"key":"cat-tree","x":1280.1,"y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":100,"y":720.1}`},
		{"/api/scene/place", `{"key":"cat-tree","x":NaN,"y":500}`},
		{"/api/scene/place", `{"key":"cat-tree","x":100,"y":Infinity}`},
		{"/api/scene/place", `{"key":"cat-tree","x":1e309,"y":500}`},
	}
	for _, tc := range cases {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			requireTaskStatus(t, w, http.StatusBadRequest)
		})
	}
}

// ===== 愿望兑换闭环：扣星 / 退星 / 兑现幂等 / 星图 =====

func wishFlowRouter(s *Server, familyID, kidID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("familyID", familyID)
		c.Set("kidID", kidID)
	})
	r.POST("/api/wish", s.createWish)
	r.GET("/api/wish/list", s.wishList)
	r.GET("/api/wish/stars", s.wishStars)
	r.POST("/api/wish/:id/redeem", s.redeemWish)
	r.POST("/api/admin/wish/:id", s.adminWishAction)
	return r
}

func wishFlowFixture(t *testing.T) (*Server, *gin.Engine, store.Kid, store.Pet) {
	t.Helper()
	db := taskIntegrationDB(t)
	fam := store.Family{Code: "778899", Name: "愿望闭环测试"}
	mustTaskCreate(t, db, &fam)
	kid := store.Kid{FamilyID: fam.ID, Name: "攒星孩子"}
	mustTaskCreate(t, db, &kid)
	pet := store.Pet{KidID: kid.ID, Name: "小橘", Species: "orange", Balance: 100, EarnedTotal: 100}
	mustTaskCreate(t, db, &pet)
	s := &Server{DB: db}
	return s, wishFlowRouter(s, fam.ID, kid.ID), kid, pet
}

func wishBalances(t *testing.T, s *Server, petID int64) (int64, int64) {
	t.Helper()
	var p store.Pet
	if err := s.DB.First(&p, petID).Error; err != nil {
		t.Fatal(err)
	}
	return p.Balance, p.EarnedTotal
}

func wishOf(t *testing.T, s *Server, id int64) store.Wish {
	t.Helper()
	var w store.Wish
	if err := s.DB.First(&w, id).Error; err != nil {
		t.Fatal(err)
	}
	return w
}

func wishEventCount(t *testing.T, s *Server, kidID int64, typ string) int64 {
	t.Helper()
	var n int64
	if err := s.DB.Model(&store.PetEvent{}).Where("kid_id=? AND type=?", kidID, typ).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// 完整链路：pending → saving → redeem_requested →(refund) saving → redeem_requested → fulfilled。
func TestWishRedeemRefundFulfillIntegration(t *testing.T) {
	s, r, kid, pet := wishFlowFixture(t)
	created := taskTestRequest(r, http.MethodPost, "/api/wish",
		map[string]any{"title": "乐高梦幻花园", "hoped_price": 50})
	requireTaskStatus(t, created, http.StatusOK)
	var c struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Status != "pending" {
		t.Fatalf("新愿望状态应为 pending，实际 %s", c.Status)
	}
	adminPath := fmt.Sprintf("/api/admin/wish/%d", c.ID)
	redeemPath := fmt.Sprintf("/api/wish/%d/redeem", c.ID)

	// 未定价不能兑换，也不能误扣星。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, redeemPath, map[string]any{}), http.StatusBadRequest)
	if b, e := wishBalances(t, s, pet.ID); b != 100 || e != 100 {
		t.Fatalf("未定价兑换不应改变余额：balance=%d earned=%d", b, e)
	}

	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "approve", "price": 50}), http.StatusOK)
	list := taskTestRequest(r, http.MethodGet, "/api/wish/list", nil)
	requireTaskStatus(t, list, http.StatusOK)
	var lr struct {
		Wishes []struct {
			CanRedeem bool  `json:"can_redeem"`
			Progress  int   `json:"progress"`
			Balance   int64 `json:"balance"`
			Price     int   `json:"price"`
		} `json:"wishes"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Wishes) != 1 || !lr.Wishes[0].CanRedeem || lr.Wishes[0].Progress != 100 || lr.Wishes[0].Price != 50 {
		t.Fatalf("攒够后列表应给出可兑换与满进度：%s", list.Body)
	}

	// 兑换：扣星 + 状态 + 事件一次事务完成，等级累计不变。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, redeemPath, map[string]any{}), http.StatusOK)
	if b, e := wishBalances(t, s, pet.ID); b != 50 || e != 100 {
		t.Fatalf("兑换应只扣可消费余额：balance=%d earned=%d", b, e)
	}
	if w := wishOf(t, s, c.ID); w.Status != "redeem_requested" || w.RedeemedAt == nil {
		t.Fatalf("兑换后状态/时间不对：%+v", w)
	}
	// 重复兑换不能二次扣星。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, redeemPath, map[string]any{}), http.StatusBadRequest)
	if b, _ := wishBalances(t, s, pet.ID); b != 50 {
		t.Fatalf("重复兑换二次扣星：balance=%d", b)
	}

	// 家长不兑现：原额退星、回攒星中、等级与收入账本不变。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "refund"}), http.StatusOK)
	if b, e := wishBalances(t, s, pet.ID); b != 100 || e != 100 {
		t.Fatalf("退回应原额返还且不动累计：balance=%d earned=%d", b, e)
	}
	if w := wishOf(t, s, c.ID); w.Status != "saving" || w.RedeemedAt != nil {
		t.Fatalf("退回后应回到攒星中并清空兑换时间：%+v", w)
	}
	if n := wishEventCount(t, s, kid.ID, "wish_refund"); n != 1 {
		t.Fatalf("退回事件应恰好一条，实际 %d", n)
	}
	var refundLedger int64
	if err := s.DB.Model(&store.SettlementLedger{}).Where("source=?", "wish_refund").Count(&refundLedger).Error; err != nil {
		t.Fatal(err)
	}
	if refundLedger != 0 {
		t.Fatalf("退回不能写收入账本，否则污染定价健康度：%d 条", refundLedger)
	}

	// 退回后愿望仍可再次兑换，然后兑现。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, redeemPath, map[string]any{}), http.StatusOK)
	if b, _ := wishBalances(t, s, pet.ID); b != 50 {
		t.Fatalf("再次兑换应扣一次：balance=%d", b)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "fulfill"}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "fulfill"}), http.StatusBadRequest)
	if n := wishEventCount(t, s, kid.ID, "wish_fulfilled"); n != 1 {
		t.Fatalf("重复兑现不应重复写猫记忆事件，实际 %d 条", n)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "refund"}), http.StatusBadRequest)
	if b, _ := wishBalances(t, s, pet.ID); b != 50 {
		t.Fatalf("已兑现愿望不能退星：balance=%d", b)
	}

	stars := taskTestRequest(r, http.MethodGet, "/api/wish/stars", nil)
	requireTaskStatus(t, stars, http.StatusOK)
	var sr struct {
		Stars []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Price int    `json:"price"`
		} `json:"stars"`
	}
	if err := json.Unmarshal(stars.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if len(sr.Stars) != 1 || sr.Stars[0].ID != c.ID || sr.Stars[0].Price != 50 {
		t.Fatalf("星图应归档本次兑现的愿望：%s", stars.Body)
	}
}

// 两个并发兑换请求只能成功一个，余额只扣一次（孩子行锁 + 状态机）。
func TestWishConcurrentRedeemChargesOnceIntegration(t *testing.T) {
	s, r, _, pet := wishFlowFixture(t)
	created := taskTestRequest(r, http.MethodPost, "/api/wish", map[string]any{"title": "并发愿望"})
	requireTaskStatus(t, created, http.StatusOK)
	var c struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	adminPath := fmt.Sprintf("/api/admin/wish/%d", c.ID)
	redeemPath := fmt.Sprintf("/api/wish/%d/redeem", c.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, adminPath,
		map[string]any{"action": "approve", "price": 60}), http.StatusOK)

	var codes [2]int
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = taskTestRequest(r, http.MethodPost, redeemPath, map[string]any{}).Code
		}(i)
	}
	wg.Wait()
	success := 0
	for _, code := range codes {
		if code == http.StatusOK {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("并发兑换应恰好一次成功，实际状态码 %v", codes)
	}
	if b, e := wishBalances(t, s, pet.ID); b != 40 || e != 100 {
		t.Fatalf("并发兑换只能扣一次：balance=%d earned=%d", b, e)
	}
}
