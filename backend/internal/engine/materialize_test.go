package engine

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSteps(t *testing.T) {
	cases := []struct {
		name, raw, legacy string
		want              []EvidenceStep
	}{
		{"旧组合", "", "photo,audio,video", []EvidenceStep{{"photo", "拍下完成情况", true}, {"audio", "说说完成情况", true}, {"video", "录下完成情况", true}}},
		{"旧组合空白", "", " photo, audio ", []EvidenceStep{{"photo", "拍下完成情况", true}, {"audio", "说说完成情况", true}}},
		{"显式清空", " [] ", "photo,audio", []EvidenceStep{}},
		{"无需材料", "", "none", []EvidenceStep{}},
		{"可选空提示", `[{"kind":"audio","label":" ","required":false}]`, "photo", []EvidenceStep{{"audio", "说说完成情况", false}}},
		{"缺省必填不丢行", `[{"kind":"photo","label":""},{"kind":"photo","label":"第二张","required":false}]`, "", []EvidenceStep{{"photo", "拍下完成情况", true}, {"photo", "第二张", false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSteps(NormalizeSteps(tc.raw, tc.legacy))
			if err != nil {
				t.Fatal(err)
			}
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(tc.want)
			if string(g) != string(w) {
				t.Fatalf("得到 %s，期望 %s", g, w)
			}
		})
	}
}

func TestParseStepsRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `"photo"`, `[`, `[null]`, `[1]`, `[{}]`,
		`[{"kind":"text","required":true}]`, `[{"kind":null}]`,
		`[{"kind":"photo","required":"false"}]`, `[{"kind":"photo","required":0}]`,
		`[{"kind":"photo","required":null}]`, `[{"kind":"audio","label":false}]`,
		`[{"kind":"audio","label":null}]`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseSteps(raw); err == nil {
				t.Fatalf("非法配置未拒绝：%s", raw)
			}
			if got := NormalizeSteps(raw, "photo"); got != raw {
				t.Fatalf("非法存量不应被默默替换：%s", got)
			}
		})
	}
}
