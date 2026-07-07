package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// —— 测试夹具 helper ——

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }

// plainNode 返回一个无循环的 claude-code 节点。
func plainNode(id string) workflow.Node {
	return workflow.Node{ID: id, DisplayName: id, Engine: "claude-code", PromptTemplate: "做事"}
}

// defWith 构造一份含给定节点的定义。
func defWith(nodes ...workflow.Node) *workflow.Definition {
	return &workflow.Definition{Name: "flow", Nodes: nodes}
}

// —— applyNodeSet：空串清除 ——

func TestApplyNodeSetClearScalarByEmptyString(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Model: "claude-opus-4-8", Effort: "high"}
	def := defWith(node)

	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Effort: strPtr("")}); err != nil {
		t.Fatalf("清除 effort 不应报错: %v", err)
	}
	got := def.Nodes[0].EngineConfig
	if got == nil || got.Model != "claude-opus-4-8" || got.Effort != "" {
		t.Fatalf("effort 应被清除、model 保留，得到 %+v", got)
	}
}

func TestApplyNodeSetClearCollapsesConfigToNil(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Model: "claude-opus-4-8"}
	def := defWith(node)

	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Model: strPtr("")}); err != nil {
		t.Fatalf("清除 model 不应报错: %v", err)
	}
	if def.Nodes[0].EngineConfig != nil {
		t.Fatalf("清空后 EngineConfig 应塌缩为 nil，得到 %+v", def.Nodes[0].EngineConfig)
	}
}

// —— applyNodeSet：evaluator 作用域 ——

func TestApplyNodeSetEvaluatorScope(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	one := 1
	node.LoopCount = &one
	def := defWith(node)

	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, Model: strPtr("claude-sonnet-5")}); err != nil {
		t.Fatalf("改评测官 model 不应报错: %v", err)
	}
	if def.Nodes[0].Evaluator.EngineConfig == nil || def.Nodes[0].Evaluator.EngineConfig.Model != "claude-sonnet-5" {
		t.Fatalf("model 应落到评测官，得到 %+v", def.Nodes[0].Evaluator.EngineConfig)
	}
	if def.Nodes[0].EngineConfig != nil {
		t.Fatalf("节点主体 EngineConfig 不应被动，得到 %+v", def.Nodes[0].EngineConfig)
	}
}

func TestApplyNodeSetDisplayNameIgnoresEvaluatorScope(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	def := defWith(node)

	// 即便带 --evaluator，--display-name 仍作用于节点级。
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, DisplayName: strPtr("生成器")}); err != nil {
		t.Fatalf("改显示名不应报错: %v", err)
	}
	if def.Nodes[0].DisplayName != "生成器" {
		t.Fatalf("displayName 应作用于节点级，得到 %q", def.Nodes[0].DisplayName)
	}
}

// 回归：节点无评测官时，--evaluator --display-name 仍应改节点级显示名，
// 不应因 --evaluator 落到「无评测循环；先挂载」的误导性拦截（--evaluator 无引擎类字段可修饰即为空操作）。
func TestApplyNodeSetDisplayNameOnNodeWithoutEvaluator(t *testing.T) {
	def := defWith(plainNode("gen"))
	outcome, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, DisplayName: strPtr("生成器")})
	if err != nil {
		t.Fatalf("无评测官节点改显示名不应报错: %v", err)
	}
	if def.Nodes[0].DisplayName != "生成器" {
		t.Fatalf("displayName 应被更新，得到 %q", def.Nodes[0].DisplayName)
	}
	if def.Nodes[0].Evaluator != nil {
		t.Fatalf("不应凭空挂上评测官，得到 %+v", def.Nodes[0].Evaluator)
	}
	if outcome.kind != outcomeUpdated {
		t.Fatalf("outcome 应为普通更新，得到 %v", outcome.kind)
	}
}

func TestApplyNodeSetEmptyDisplayNameRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{DisplayName: strPtr("")}); err == nil {
		t.Fatal("空 displayName 应报错")
	}
}

// —— applyNodeSet：新建 evaluator 默认值 ——

func TestApplyNodeSetMountEvaluatorDefaults(t *testing.T) {
	def := defWith(plainNode("gen"))
	outcome, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, Engine: strPtr("claude-code")})
	if err != nil {
		t.Fatalf("挂载评测官不应报错: %v", err)
	}
	if outcome.kind != outcomeMountedEvaluator {
		t.Fatalf("outcome 应为挂载评测循环，得到 %v", outcome.kind)
	}
	ev := def.Nodes[0].Evaluator
	if ev == nil || ev.Engine != "claude-code" {
		t.Fatalf("应挂上 claude-code 评测官，得到 %+v", ev)
	}
	if ev.PromptTemplate != workflow.DefaultEvaluatorPrompt {
		t.Fatalf("默认提示词应为 DefaultEvaluatorPrompt，得到 %q", ev.PromptTemplate)
	}
	if def.Nodes[0].LoopCount == nil || *def.Nodes[0].LoopCount != 1 {
		t.Fatalf("LoopCount 应默认 1，得到 %v", def.Nodes[0].LoopCount)
	}
}

// 回归：导入定义可携带「无循环却有 loopCount」的休眠值（validate.go 故意放行）；
// 挂载评测官且未显式给 loop-count 时，须以默认 1 覆盖休眠值，不能让它被静默激活成循环次数。
func TestApplyNodeSetMountEvaluatorOverridesDormantLoopCount(t *testing.T) {
	node := plainNode("gen")
	node.LoopCount = intPtr(3) // 休眠值：此刻既无 evaluator 也无 redoTarget
	def := defWith(node)

	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, Engine: strPtr("claude-code")}); err != nil {
		t.Fatalf("挂载评测官不应报错: %v", err)
	}
	if def.Nodes[0].LoopCount == nil || *def.Nodes[0].LoopCount != 1 {
		t.Fatalf("挂载时未给 loop-count，休眠值应被默认 1 覆盖，得到 %v", def.Nodes[0].LoopCount)
	}
}

func TestApplyNodeSetMountEvaluatorWithoutEngineRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, Model: strPtr("x")}); err == nil {
		t.Fatal("节点无评测官又无 --engine 时应报错")
	}
}

// —— applyNodeSet：redoTarget 挂载 + loopCount 默认 ——

func TestApplyNodeSetMountRedoTargetDefaults(t *testing.T) {
	def := defWith(plainNode("gen"), plainNode("review"))
	outcome, err := applyNodeSet(def, "review", nodeSetOptions{RedoTarget: strPtr("gen")})
	if err != nil {
		t.Fatalf("挂载回跳不应报错: %v", err)
	}
	if outcome.kind != outcomeMountedRedo || outcome.redoTarget != "gen" {
		t.Fatalf("outcome 应为挂载回跳→gen，得到 %+v", outcome)
	}
	if def.Nodes[1].RedoTarget != "gen" {
		t.Fatalf("RedoTarget 应设为 gen，得到 %q", def.Nodes[1].RedoTarget)
	}
	if def.Nodes[1].LoopCount == nil || *def.Nodes[1].LoopCount != 1 {
		t.Fatalf("LoopCount 应默认 1，得到 %v", def.Nodes[1].LoopCount)
	}
}

// —— applyNodeSet：拆除 ——

func TestApplyNodeSetNoEvaluator(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	one := 1
	node.LoopCount = &one
	def := defWith(node)

	outcome, err := applyNodeSet(def, "gen", nodeSetOptions{NoEvaluator: true})
	if err != nil {
		t.Fatalf("拆评测官不应报错: %v", err)
	}
	if outcome.kind != outcomeUnmountedEvaluator {
		t.Fatalf("outcome 应为拆评测循环，得到 %v", outcome.kind)
	}
	if def.Nodes[0].Evaluator != nil || def.Nodes[0].LoopCount != nil {
		t.Fatalf("评测官与 LoopCount 应清空，得到 ev=%+v loop=%v", def.Nodes[0].Evaluator, def.Nodes[0].LoopCount)
	}
}

func TestApplyNodeSetNoRedo(t *testing.T) {
	node := plainNode("review")
	node.RedoTarget = "gen"
	one := 3
	node.LoopCount = &one
	def := defWith(plainNode("gen"), node)

	outcome, err := applyNodeSet(def, "review", nodeSetOptions{NoRedo: true})
	if err != nil {
		t.Fatalf("拆回跳不应报错: %v", err)
	}
	if outcome.kind != outcomeUnmountedRedo {
		t.Fatalf("outcome 应为拆回跳，得到 %v", outcome.kind)
	}
	if def.Nodes[1].RedoTarget != "" || def.Nodes[1].LoopCount != nil {
		t.Fatalf("RedoTarget 与 LoopCount 应清空，得到 target=%q loop=%v", def.Nodes[1].RedoTarget, def.Nodes[1].LoopCount)
	}
}

func TestApplyNodeSetUnmountNonexistentRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{NoEvaluator: true}); err == nil {
		t.Fatal("拆一个本就不存在的评测循环应报错")
	}
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{NoRedo: true}); err == nil {
		t.Fatal("拆一个本就不存在的回跳应报错")
	}
}

// —— applyNodeSet：互斥 ——

func TestApplyNodeSetMountEvaluatorOnRedoNodeRejected(t *testing.T) {
	node := plainNode("review")
	node.RedoTarget = "gen"
	def := defWith(plainNode("gen"), node)
	if _, err := applyNodeSet(def, "review", nodeSetOptions{Evaluator: true, Engine: strPtr("claude-code")}); err == nil {
		t.Fatal("节点已配 redoTarget 时挂 evaluator 应因互斥报错")
	}
}

func TestApplyNodeSetMountRedoOnEvaluatorNodeRejected(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	def := defWith(plainNode("plan"), node)
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{RedoTarget: strPtr("plan")}); err == nil {
		t.Fatal("节点已有 evaluator 时挂 redoTarget 应因互斥报错")
	}
}

// —— applyNodeSet：节点不存在 ——

func TestApplyNodeSetNodeNotFound(t *testing.T) {
	def := defWith(plainNode("gen"))
	if _, err := applyNodeSet(def, "ghost", nodeSetOptions{Model: strPtr("x")}); err == nil {
		t.Fatal("节点不存在应报错")
	}
}

// —— applyNodeSet：命令级 loop-count 施于无循环节点退 1 ——

func TestApplyNodeSetLoopCountOnPlainNodeRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{LoopCount: intPtr(5)}); err == nil {
		t.Fatal("对无循环节点设 loop-count 应命令级退错")
	}
}

func TestApplyNodeSetLoopCountWithMountAllowed(t *testing.T) {
	def := defWith(plainNode("gen"), plainNode("review"))
	if _, err := applyNodeSet(def, "review", nodeSetOptions{RedoTarget: strPtr("gen"), LoopCount: intPtr(5)}); err != nil {
		t.Fatalf("同命令挂回跳 + loop-count 不应报错: %v", err)
	}
	if def.Nodes[1].LoopCount == nil || *def.Nodes[1].LoopCount != 5 {
		t.Fatalf("显式 loop-count 应覆盖默认，得到 %v", def.Nodes[1].LoopCount)
	}
}

// —— applyNodeSet + Validate：改 engine 级联不兼容由整份校验捕获 ——

func TestApplyNodeSetEngineSwitchCaughtByValidate(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Effort: "high"} // claude-code 专属
	def := defWith(node)

	// 只改 engine 为 qoder，不清 effort → applyNodeSet 不预判，交整份校验。
	if _, err := applyNodeSet(def, "gen", nodeSetOptions{Engine: strPtr("qoder")}); err != nil {
		t.Fatalf("applyNodeSet 不应预判引擎兼容性: %v", err)
	}
	if err := workflow.Validate(def); err == nil {
		t.Fatal("qoder 不认 effort，整份校验应报错")
	}
}

// —— checkNodeSetFlagCombo：四类退 2 ——

func TestCheckNodeSetFlagComboNoOperation(t *testing.T) {
	// --evaluator 单独给不计作操作。
	if err := checkNodeSetFlagCombo(nodeSetOptions{Evaluator: true}); err == nil {
		t.Fatal("无任何字段 / 拆除选项应报用法错误")
	} else if _, ok := err.(*usageError); !ok {
		t.Fatalf("应为 usageError（退 2），得到 %T", err)
	}
}

func TestCheckNodeSetFlagComboTeardownNotStandalone(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{NoEvaluator: true, Model: strPtr("x")}); err == nil {
		t.Fatal("--no-evaluator 与字段选项同用应报用法错误")
	} else if _, ok := err.(*usageError); !ok {
		t.Fatalf("应为 usageError（退 2），得到 %T", err)
	}
	if err := checkNodeSetFlagCombo(nodeSetOptions{NoRedo: true, Evaluator: true}); err == nil {
		t.Fatal("--no-redo 与 --evaluator 同用应报用法错误")
	}
}

func TestCheckNodeSetFlagComboBothTeardown(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{NoEvaluator: true, NoRedo: true}); err == nil {
		t.Fatal("两个 --no-* 同给应报用法错误")
	} else if _, ok := err.(*usageError); !ok {
		t.Fatalf("应为 usageError（退 2），得到 %T", err)
	}
}

func TestCheckNodeSetFlagComboRedoTargetEmpty(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{RedoTarget: strPtr("")}); err == nil {
		t.Fatal("--redo-target \"\" 应报用法错误")
	} else if _, ok := err.(*usageError); !ok {
		t.Fatalf("应为 usageError（退 2），得到 %T", err)
	} else if !strings.Contains(err.Error(), "--no-redo") {
		t.Fatalf("应提示改用 --no-redo，得到 %q", err.Error())
	}
}

func TestCheckNodeSetFlagComboValidPasses(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{Model: strPtr("x")}); err != nil {
		t.Fatalf("合法组合不应报错: %v", err)
	}
	if err := checkNodeSetFlagCombo(nodeSetOptions{NoEvaluator: true}); err != nil {
		t.Fatalf("单独 --no-evaluator 不应报错: %v", err)
	}
}

// —— applyNodeSet：--evaluator 仅伴 --display-name（无引擎类字段）应归普通更新，文案不误报「评测官」——

func TestApplyNodeSetEvaluatorScopeDisplayNameOnlyIsPlainUpdate(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	def := defWith(node)

	outcome, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, DisplayName: strPtr("生成器")})
	if err != nil {
		t.Fatalf("改显示名不应报错: %v", err)
	}
	// displayName 恒节点级、评测官分毫未动，故归 outcomeUpdated 而非 outcomeUpdatedEvaluator。
	if outcome.kind != outcomeUpdated {
		t.Fatalf("仅改节点级 displayName 应归普通更新，得到 %v", outcome.kind)
	}
}

func TestApplyNodeSetEvaluatorScopeWithEngineFieldIsEvaluatorUpdate(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	def := defWith(node)

	outcome, err := applyNodeSet(def, "gen", nodeSetOptions{Evaluator: true, Model: strPtr("claude-sonnet-5"), DisplayName: strPtr("生成器")})
	if err != nil {
		t.Fatalf("改评测官 model + 节点显示名不应报错: %v", err)
	}
	if outcome.kind != outcomeUpdatedEvaluator {
		t.Fatalf("有引擎类字段落到评测官应归更新评测官，得到 %v", outcome.kind)
	}
}

// —— printNodeSetOutcome：锁定 spec〈输出〉逐字规定的用户可见成功文案 ——

func TestPrintNodeSetOutcomeContractStrings(t *testing.T) {
	cases := []struct {
		outcome nodeSetOutcome
		want    string
	}{
		{nodeSetOutcome{kind: outcomeMountedEvaluator}, "✓ 已为 flow·gen 挂载评测循环\n"},
		{nodeSetOutcome{kind: outcomeMountedRedo, redoTarget: "plan"}, "✓ 已为 flow·gen 挂载回跳→plan\n"},
		{nodeSetOutcome{kind: outcomeUnmountedEvaluator}, "✓ 已拆除 flow·gen 的评测循环\n"},
		{nodeSetOutcome{kind: outcomeUnmountedRedo}, "✓ 已拆除 flow·gen 的回跳\n"},
		{nodeSetOutcome{kind: outcomeUpdatedEvaluator}, "✓ 已更新 flow·gen 的评测官\n"},
		{nodeSetOutcome{kind: outcomeUpdated}, "✓ 已更新 flow·gen\n"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		printNodeSetOutcome(cmd, "flow", "gen", c.outcome)
		if got := buf.String(); got != c.want {
			t.Fatalf("outcome %v 文案应为 %q，得到 %q", c.outcome.kind, c.want, got)
		}
	}
}
