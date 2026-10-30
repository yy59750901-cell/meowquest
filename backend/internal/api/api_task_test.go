package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/engine"
)

func taskTestRouter(s *Server, familyID, kidID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("familyID", familyID)
		c.Set("kidID", kidID)
	})
	r.POST("/api/admin/task", s.createTask)
	r.PUT("/api/admin/task/:id", s.updateTask)
	r.POST("/api/tasks/:id/submit", s.submitTask)
	r.POST("/api/admin/confirm/:id", s.confirmTask)
	r.GET("/api/tasks/today", s.todayTasks)
	return r
}

func taskTestRequest(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTaskRequestSteps(t *testing.T) {
	cases := []struct {
		name     string
		body     map[string]any
		count    int
		required bool
	}{
		{"旧客户端未提供", map[string]any{"evidence_type": "photo,audio"}, 2, true},
		{"显式空数组", map[string]any{"evidence_type": "photo,audio", "evidence_steps": "[]"}, 0, false},
		{"显式空字符串", map[string]any{"evidence_type": "photo", "evidence_steps": ""}, 0, false},
		{"可选空提示", map[string]any{"evidence_steps": `[{"kind":"audio","label":"","required":false}]`}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			var req taskReq
			if err := json.Unmarshal(b, &req); err != nil {
				t.Fatal(err)
			}
			tpl, err := req.toTemplate()
			if err != nil {
				t.Fatal(err)
			}
			steps, err := engine.ParseSteps(tpl.EvidenceSteps)
			if err != nil {
				t.Fatal(err)
			}
			if len(steps) != tc.count {
				t.Fatalf("步骤数量 %d，期望 %d", len(steps), tc.count)
			}
			for _, step := range steps {
				if step.Required != tc.required || step.Label == "" {
					t.Fatalf("步骤错误：%+v", step)
				}
			}
		})
	}
}

func TestCreateAndEditRejectInvalidStepsBeforeWriting(t *testing.T) {
	r := taskTestRouter(&Server{}, 1, 1)
	for _, steps := range []any{nil, []any{}, 3, "null", "{}", "[", "[null]", `[{"kind":"text"}]`, `[{"kind":"photo","required":"false"}]`, `[{"kind":"audio","required":null}]`, `[{"kind":"video","label":null}]`, `[{"kind":"photo","label":123}]`, `[{"kind":"photo"}] garbage`} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			path := "/api/admin/task"
			if method == http.MethodPut {
				path += "/1"
			}
			w := taskTestRequest(r, method, path, map[string]any{"name": "测试", "evidence_steps": steps})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s %v 返回 %d：%s", method, steps, w.Code, w.Body)
			}
		}
	}
}

func TestMissingRequiredSteps(t *testing.T) {
	cases := []struct {
		name, steps, legacy, evidence, missing string
		invalid                                bool
	}{
		{"空提示仍必填", `[{"kind":"photo","label":"","required":true}]`, "", "[]", "拍下完成情况", false},
		{"可选步骤", `[{"kind":"audio","label":"","required":false}]`, "", "[]", "", false},
		{"缺省必填", `[{"kind":"video"}]`, "", "[]", "录下完成情况", false},
		{"旧组合", "", "photo,audio", `[{"type":"photo","path":"/uploads/a.jpg"}]`, "说说完成情况", false},
		{"显式空不复活", "[]", "photo,audio", "[]", "", false},
		{"完整材料", `[{"kind":"audio","required":true}]`, "", `[{"type":"audio","path":"/uploads/a.wav"}]`, "", false},
		{"无文件不算材料", `[{"kind":"photo","required":true}]`, "", `[{"type":"photo"}]`, "拍下完成情况", false},
		{"空白文件不算材料", `[{"kind":"photo","label":" "}]`, "", `[{"type":"photo","path":"  "}]`, "拍下完成情况", false},
		{"同类必填不能复用一份", `[{"kind":"photo"},{"kind":"photo","label":"第二张"}]`, "", `[{"type":"photo","path":"/uploads/one.jpg"}]`, "第二张", false},
		{"可选不消耗必填材料", `[{"kind":"photo","required":false},{"kind":"photo"}]`, "", `[{"type":"photo","path":"/uploads/one.jpg"}]`, "", false},
		{"旧视频组合", "", "photo,video", `[{"type":"photo","path":"/uploads/a.jpg"}]`, "录下完成情况", false},
		{"材料空对象元素", "[]", "", "[null]", "", true},
		{"材料错误元素类型", "[]", "", "[1]", "", true},
		{"材料错误字段类型", "[]", "", `[{"type":"photo","path":1}]`, "", true},
		{"材料非数组", "[]", "", "{}", "", true},
		{"材料null", "[]", "", "null", "", true},
		{"材料非法JSON", "[]", "", "[", "", true},
		{"配置非法JSON", "[", "", "[]", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, _ := json.Marshal(engine.Snapshot{EvidenceSteps: tc.steps, EvidenceType: tc.legacy})
			missing, err := missingRequiredSteps(string(snapshot), tc.evidence)
			if (err != nil) != tc.invalid || missing != tc.missing {
				t.Fatalf("得到缺失 %q，错误 %v", missing, err)
			}
		})
	}
	if _, err := missingRequiredSteps("{", "[]"); err == nil || !strings.Contains(err.Error(), "快照") {
		t.Fatal("非法快照未拒绝")
	}
}
