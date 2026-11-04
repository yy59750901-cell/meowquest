package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
