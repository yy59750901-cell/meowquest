package api

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/store"
)

func TestLitterAtomicExpressions(t *testing.T) {
	use := litterUseExpr()
	if use.SQL != "LEAST(?, state + 1)" || len(use.Vars) != 1 || use.Vars[0] != litterMaxState {
		t.Fatalf("use 必须原子 +1 并封顶 3，实际 SQL=%q Vars=%v", use.SQL, use.Vars)
	}
	scoop := litterScoopExpr(2)
	if scoop.SQL != "GREATEST(0, state - ?)" || len(scoop.Vars) != 1 || scoop.Vars[0] != 2 {
		t.Fatalf("scoop 必须原子扣减且不为负，实际 SQL=%q Vars=%v", scoop.SQL, scoop.Vars)
	}
}

func TestClampLitterState(t *testing.T) {
	cases := map[int]int{-5: 0, 0: 0, 1: 1, 3: 3, 4: 3, 99: 3}
	for in, want := range cases {
		if got := clampLitterState(in); got != want {
			t.Errorf("clampLitterState(%d)=%d, 期望 %d", in, got, want)
		}
	}
}

// 参数校验必须在触达 DB 之前完成：Server 不带 DB，若误用 DB 会直接 panic。
func TestLitterHandlersRejectInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("kidID", int64(7)) })
	s := &Server{}
	r.POST("/api/litter/use", s.litterUse)
	r.POST("/api/litter/scoop", s.litterScoop)

	cases := []struct {
		path string
		body map[string]any
	}{
		{"/api/litter/use", map[string]any{}},
		{"/api/litter/use", map[string]any{"id": 0}},
		{"/api/litter/scoop", map[string]any{"id": 1}},
		{"/api/litter/scoop", map[string]any{"id": 1, "delta": 0}},
		{"/api/litter/scoop", map[string]any{"id": 1, "delta": 4}},
	}
	for _, tc := range cases {
		w := taskTestRequest(r, http.MethodPost, tc.path, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %v 应返回 400，实际 %d %s", tc.path, tc.body, w.Code, w.Body.String())
		}
	}
}

func TestLitterItemRegistered(t *testing.T) {
	for _, it := range ITEM_REGISTRY {
		if it["key"] == litterItemKey {
			if it["price"] != 30 || it["type"] != "furniture" {
				t.Fatalf("猫砂盆商品配置不符：%v", it)
			}
			return
		}
	}
	t.Fatal("ITEM_REGISTRY 未注册猫砂盆")
}

func TestLitterStateFieldIsPersistedAndMigrated(t *testing.T) {
	f, ok := reflect.TypeOf(store.SceneItem{}).FieldByName("State")
	if !ok || f.Type.Kind() != reflect.Int {
		t.Fatal("SceneItem 缺少 int 型 State 字段，状态不会被 /api/scene 带出")
	}
	if !strings.Contains(string(f.Tag), `json:"state"`) || !strings.Contains(string(f.Tag), "column:state") {
		t.Fatalf("State 字段 tag 必须含 json/column=state，实际 %s", f.Tag)
	}
	// 测试工作目录是 backend/internal/api
	src, err := os.ReadFile("../store/store.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "&SceneItem{}") {
		t.Fatal("AutoMigrate 未包含 SceneItem，加列不会自动生效")
	}
}

func TestLitterRoutesAndChatEventText(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{`api.POST("/litter/use"`, `api.POST("/litter/scoop"`} {
		if !strings.Contains(string(src), route) {
			t.Errorf("路由未注册：%s", route)
		}
	}
	text := eventCNSafe("litter_scoop", map[string]any{}, "2026-11-09")
	if !strings.Contains(text, "铲") {
		t.Fatalf("litter_scoop 事件在聊天里会被吞成空串，实际 %q", text)
	}
}

// 商店顺序就是 ITEM_REGISTRY 的顺序（前端不排序），锁住「能玩的先露面」防止回归。
func TestShopOrderPlayablesFirst(t *testing.T) {
	want := []string{"litter-box", "cat-tree", "yarn", "plant", "fish"}
	if len(ITEM_REGISTRY) <= len(want) {
		t.Fatalf("商品数量异常: %d", len(ITEM_REGISTRY))
	}
	for i, key := range want {
		if got := ITEM_REGISTRY[i]["key"]; got != key {
			t.Errorf("商店第 %d 张卡应为 %q，实际 %v", i+1, key, got)
		}
	}
	nests := 0
	for i, it := range ITEM_REGISTRY {
		key := it["key"].(string)
		if strings.HasPrefix(key, "nest") {
			nests++
			if i < len(want) {
				t.Errorf("猫窝 %q 混进了前 %d 张卡", key, len(want))
			}
		}
	}
	if nests != 17 {
		t.Errorf("猫窝数量应为 17（含切面苹果窝），实际 %d", nests)
	}
	if got := ITEM_REGISTRY[len(ITEM_REGISTRY)-1]["key"]; got != "wp-mint" {
		t.Errorf("末尾应为壁纸 wp-mint，实际 %v", got)
	}
}
