package workflow

import "testing"

func TestRender(t *testing.T) {
	sysVars := map[string]string{"userPrompt": "加个按钮", "cwd": "/proj"}
	artifacts := map[string]string{"plan": "方案X"}
	valid := func(id string) bool { return id == "plan" || id == "code" }

	cases := []struct {
		name     string
		template string
		want     string
	}{
		{"系统变量", "需求：{{sys.userPrompt}}", "需求：加个按钮"},
		{"cwd", "在 {{sys.cwd}} 工作", "在 /proj 工作"},
		{"节点产物", "上游：{{plan}}", "上游：方案X"},
		{"合法节点未跑取空串", "码：{{code}}。", "码：。"},
		{"转义保留字面量", `字面 \{{plan}}`, "字面 {{plan}}"},
		{"未注入系统变量保留字面量", "{{sys.unknown}}", "{{sys.unknown}}"},
		{"非法引用保留字面量", "{{ghost}}", "{{ghost}}"},
		{"混合", "{{sys.userPrompt}} / {{plan}} / {{ghost}}", "加个按钮 / 方案X / {{ghost}}"},
		{"无变量原样返回", "纯文本", "纯文本"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Render(c.template, sysVars, artifacts, valid); got != c.want {
				t.Errorf("Render(%q) = %q，期望 %q", c.template, got, c.want)
			}
		})
	}
}
