// Package message 把机器稳定领域错误渲染为 CLI 产品文案。
package message

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/locale"
)

// Error 渲染结构化错误；技术详情保持原样。
func Error(language locale.Language, err *apperror.Error) string {
	if err.TechnicalDetail != "" {
		return err.TechnicalDetail
	}
	if err.Code == apperror.CodeWorkflowValidationFailed {
		lines := make([]string, 0, len(err.Problems)+1)
		lines = append(lines, language.Select("工作流定义校验未通过", "workflow definition validation failed"))
		for _, problem := range err.Problems {
			lines = append(lines, problem.Path+": "+Problem(language, problem))
		}
		return strings.Join(lines, "\n")
	}
	return render(language, err.Code, err.Params)
}

// Problem 渲染单条结构化校验问题。
func Problem(language locale.Language, problem apperror.Problem) string {
	return render(language, problem.Code, problem.Params)
}

func render(language locale.Language, code apperror.Code, params apperror.Params) string {
	p := parameterReader{params: params}
	zh, en := "", ""
	switch code {
	case apperror.CodeTechnicalFailure:
		zh, en = "技术操作失败", "technical operation failed"
	case apperror.CodeWorkflowValidationFailed:
		zh, en = "工作流定义校验未通过", "workflow definition validation failed"
	case apperror.CodeWorkflowNotFound:
		zh, en = fmt.Sprintf("工作流 %s 不存在", p.text("name")), fmt.Sprintf("workflow %s does not exist", p.text("name"))
	case apperror.CodeWorkflowAlreadyExists:
		zh, en = fmt.Sprintf("工作流 %s 已存在", p.text("name")), fmt.Sprintf("workflow %s already exists", p.text("name"))
	case apperror.CodeRunNotFound:
		zh, en = fmt.Sprintf("运行 %s 不存在", p.text("id")), fmt.Sprintf("run %s does not exist", p.text("id"))
	case apperror.CodeRunAlreadyExists:
		zh, en = fmt.Sprintf("运行 %s 已存在", p.text("id")), fmt.Sprintf("run %s already exists", p.text("id"))
	case apperror.CodeRunSummaryNotFound:
		zh, en = "运行总结尚未生成", "run summary has not been generated"
	case apperror.CodeWorkingDirectoryNotFound:
		zh, en = "工作目录不存在："+p.text("path"), "working directory does not exist: "+p.text("path")
	case apperror.CodeWorkingDirectoryNotDir:
		zh, en = "工作目录不是目录："+p.text("path"), "working directory is not a directory: "+p.text("path")
	case apperror.CodeWorkflowNameInvalidChars:
		zh = fmt.Sprintf("工作流名 %q 非法：只允许字母、数字、点、下划线、连字符（[A-Za-z0-9._-]+）", p.text("name"))
		en = fmt.Sprintf("invalid workflow name %q: only letters, digits, dots, underscores, and hyphens are allowed ([A-Za-z0-9._-]+)", p.text("name"))
	case apperror.CodeWorkflowNameReserved:
		zh, en = fmt.Sprintf("工作流名 %q 非法：不能是 . 或 ..", p.text("name")), fmt.Sprintf("invalid workflow name %q: it cannot be . or ..", p.text("name"))
	case apperror.CodeRunIDInvalid:
		zh = fmt.Sprintf("run id %q 非法：只允许字母、数字、点、下划线、连字符，且不能是 . 或 ..", p.text("id"))
		en = fmt.Sprintf("invalid run id %q: only letters, digits, dots, underscores, and hyphens are allowed, and it cannot be . or ..", p.text("id"))
	case apperror.CodeRunNotStoppable:
		zh = fmt.Sprintf("运行 %s 当前状态为 %s，无可终止（仅 running 可终止）", p.text("id"), p.text("status"))
		en = fmt.Sprintf("run %s is %s and cannot be stopped (only running runs can be stopped)", p.text("id"), p.text("status"))
	case apperror.CodeRunNotResumable:
		switch p.text("status") {
		case "completed":
			zh, en = fmt.Sprintf("%s: 已成功完成，无需恢复", p.text("id")), fmt.Sprintf("%s: completed successfully; no resume is needed", p.text("id"))
		case "running":
			zh, en = fmt.Sprintf("%s: 仍在运行中，无法恢复", p.text("id")), fmt.Sprintf("%s: still running and cannot be resumed", p.text("id"))
		default:
			zh = fmt.Sprintf("运行 %s 当前状态为 %s，无法恢复（仅 failed / interrupted 可恢复）", p.text("id"), p.text("status"))
			en = fmt.Sprintf("run %s is %s and cannot be resumed (only failed / interrupted runs can be resumed)", p.text("id"), p.text("status"))
		}
	case apperror.CodeRunNotDeletable:
		zh = fmt.Sprintf("运行 %s 仍在进行中，无法删除；请先 conduct run stop %s 终止再删", p.text("id"), p.text("id"))
		en = fmt.Sprintf("run %s is still in progress and cannot be deleted; stop it first with conduct run stop %s", p.text("id"), p.text("id"))
	case apperror.CodeWorkflowSaveConflict:
		zh, en = "定义已被外部修改，保存基线过期", "the definition was modified externally and the save baseline is stale"
	case apperror.CodeUserPromptRequired:
		zh, en = "缺少用户需求：不能为空", "user request is required and cannot be empty"
	case apperror.CodeWorkingDirectoryMustBeAbs:
		zh, en = "工作目录必须是绝对路径（以 / 开头）："+p.text("path"), "working directory must be an absolute path (starting with /): "+p.text("path")
	case apperror.CodeNodeNotFound:
		zh, en = fmt.Sprintf("工作流无节点 %s", p.text("id")), fmt.Sprintf("workflow has no node %s", p.text("id"))
	case apperror.CodeNodeAlreadyExists:
		zh, en = fmt.Sprintf("已存在同名节点 %q", p.text("id")), fmt.Sprintf("a node named %q already exists", p.text("id"))
	case apperror.CodeReservedNodeID:
		switch p.text("action") {
		case "remove":
			zh, en = fmt.Sprintf("保留标记节点 %s 不能删除", p.text("id")), fmt.Sprintf("reserved marker node %s cannot be removed", p.text("id"))
		case "rename":
			zh, en = fmt.Sprintf("保留标记节点 %s 不能改名", p.text("id")), fmt.Sprintf("reserved marker node %s cannot be renamed", p.text("id"))
		default:
			zh, en = fmt.Sprintf("节点 id 不得为保留名 %s", p.text("id")), fmt.Sprintf("node id must not use reserved name %s", p.text("id"))
		}
	case apperror.CodeInvalidNodeID:
		zh = fmt.Sprintf("节点 id %q 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）", p.text("id"))
		en = fmt.Sprintf("invalid node id %q (must match ^[A-Za-z_][A-Za-z0-9_-]{0,63}$)", p.text("id"))
	case apperror.CodeNodeDisplayNameRequired:
		zh, en = fmt.Sprintf("节点 %s 的 displayName 不能为空", p.text("id")), fmt.Sprintf("node %s displayName cannot be empty", p.text("id"))
	case apperror.CodeUnsupportedMediaType:
		zh, en = "变更类请求必须为 application/json", "mutating requests must use application/json"
	case apperror.CodeForbiddenOrigin:
		zh = fmt.Sprintf("拒绝：%s %q 不在白名单（仅限本机访问）", p.text("kind"), p.text("value"))
		en = fmt.Sprintf("rejected: %s %q is not allowlisted (local access only)", p.text("kind"), p.text("value"))
	case apperror.CodeInvalidSettingsRequest:
		switch p.text("reason") {
		case "body_object":
			zh, en = "请求体必须是 JSON 对象", "request body must be a JSON object"
		case "language_required":
			zh, en = "请求体必须包含 language", "request body must contain language"
		case "unknown_field":
			zh, en = "请求体包含未知字段："+p.text("field"), "request body contains unknown field: "+p.text("field")
		case "language_type":
			zh, en = "language 必须是 null、en 或 zh-CN", "language must be null, en, or zh-CN"
		default:
			zh, en = "设置请求无效", "invalid settings request"
		}
	case apperror.CodeInvalidRequest:
		zh, en = "请求体 JSON 解析失败", "failed to parse request body JSON"
	case apperror.CodeDirectoryNotFound:
		zh, en = "目录不存在："+p.text("path"), "directory does not exist: "+p.text("path")
	case apperror.CodePathNotDirectory:
		zh, en = "不是目录："+p.text("path"), "path is not a directory: "+p.text("path")
	case apperror.CodeEdgeNotFound:
		zh, en = fmt.Sprintf("删不存在的边 %s→%s", p.text("from"), p.text("to")), fmt.Sprintf("cannot remove nonexistent edge %s→%s", p.text("from"), p.text("to"))
	case apperror.CodeEdgeAlreadyExists:
		zh, en = fmt.Sprintf("加已存在的边 %s→%s", p.text("from"), p.text("to")), fmt.Sprintf("cannot add existing edge %s→%s", p.text("from"), p.text("to"))
	case apperror.CodeNodesRequired:
		zh, en = "不能为空，至少需要一个节点", "cannot be empty; at least one node is required"
	case apperror.CodeDuplicateNodeID:
		zh, en = fmt.Sprintf("与前面的节点重复 %q", p.text("id")), fmt.Sprintf("duplicates an earlier node %q", p.text("id"))
	case apperror.CodeStartNodeCount:
		zh, en = fmt.Sprintf("须恰好含一个 START 标记节点，得到 %d 个", p.integer("count")), fmt.Sprintf("must contain exactly one START marker node; got %d", p.integer("count"))
	case apperror.CodeEndNodeCount:
		zh, en = fmt.Sprintf("须恰好含一个 END 标记节点，得到 %d 个", p.integer("count")), fmt.Sprintf("must contain exactly one END marker node; got %d", p.integer("count"))
	case apperror.CodeAgentNodeRequired:
		zh, en = "至少需要一个 agent 节点（START / END 之外）", "at least one agent node besides START / END is required"
	case apperror.CodeMarkerFieldNotEmpty:
		zh = fmt.Sprintf("标记节点 %s 的 %s 必须为空", p.text("id"), p.text("field"))
		en = fmt.Sprintf("%s must be empty on marker node %s", p.text("field"), p.text("id"))
	case apperror.CodeRequiredField:
		zh, en = "必填", "required"
	case apperror.CodeEdgeEndpointsRequired:
		zh, en = "from / to 不能为空", "from / to cannot be empty"
	case apperror.CodeEdgeFromNodeNotFound:
		zh, en = fmt.Sprintf("from 指向不存在的节点 %q", p.text("id")), fmt.Sprintf("from points to nonexistent node %q", p.text("id"))
	case apperror.CodeEdgeToNodeNotFound:
		zh, en = fmt.Sprintf("to 指向不存在的节点 %q", p.text("id")), fmt.Sprintf("to points to nonexistent node %q", p.text("id"))
	case apperror.CodeSelfEdge:
		zh, en = fmt.Sprintf("禁止自环 %s→%s", p.text("from"), p.text("to")), fmt.Sprintf("self-edge %s→%s is forbidden", p.text("from"), p.text("to"))
	case apperror.CodeStartEndDirectEdge:
		zh, en = "禁止 START→END 直连（须过 ≥1 个 agent 节点）", "a direct START→END edge is forbidden (must pass through at least one agent node)"
	case apperror.CodeEdgeToStart:
		zh, en = "禁止边指向 START（START 无入边）", "an edge to START is forbidden (START has no incoming edges)"
	case apperror.CodeEdgeFromEnd:
		zh, en = "禁止边源自 END（END 无出边）", "an edge from END is forbidden (END has no outgoing edges)"
	case apperror.CodeDuplicateEdge:
		zh, en = fmt.Sprintf("重复边 %s→%s", p.text("from"), p.text("to")), fmt.Sprintf("duplicate edge %s→%s", p.text("from"), p.text("to"))
	case apperror.CodeCycleDetected:
		zh, en = "检测到环 "+p.text("cycle"), "cycle detected: "+p.text("cycle")
	case apperror.CodeNodeMissingIncomingEdge:
		zh = fmt.Sprintf("agent 节点 %q 无入边（须 ≥1 条，可来自 START）", p.text("id"))
		en = fmt.Sprintf("agent node %q has no incoming edge (at least one is required and may come from START)", p.text("id"))
	case apperror.CodeNodeMissingOutgoingEdge:
		zh = fmt.Sprintf("agent 节点 %q 无出边（须 ≥1 条，可到 END）", p.text("id"))
		en = fmt.Sprintf("agent node %q has no outgoing edge (at least one is required and may go to END)", p.text("id"))
	case apperror.CodeUnknownSystemVariable:
		zh = fmt.Sprintf("引用未知系统变量 {{%s}}（仅支持 sys.userPrompt / sys.cwd / sys.runId）", p.text("key"))
		en = fmt.Sprintf("references unknown system variable {{%s}} (only sys.userPrompt / sys.cwd / sys.runId are supported)", p.text("key"))
	case apperror.CodeMarkerNodeReference:
		zh, en = fmt.Sprintf("禁止引用标记节点 {{%s}}（无产物）", p.text("id")), fmt.Sprintf("referencing marker node {{%s}} is forbidden (it has no artifact)", p.text("id"))
	case apperror.CodeNonAncestorNodeReference:
		zh = fmt.Sprintf("引用非上游祖先节点 {{%s}}（数据流须来自沿边可达的前驱）", p.text("id"))
		en = fmt.Sprintf("references non-upstream-ancestor node {{%s}} (data must come from a predecessor reachable along edges)", p.text("id"))
	case apperror.CodeNodeReferenceNotFound:
		zh, en = fmt.Sprintf("引用不存在的节点 {{%s}}", p.text("id")), fmt.Sprintf("references nonexistent node {{%s}}", p.text("id"))
	case apperror.CodeUnknownEngine:
		zh = fmt.Sprintf("未知引擎 %q（可用：%s）", p.text("engine"), p.text("available"))
		en = fmt.Sprintf("unknown engine %q (available: %s)", p.text("engine"), p.text("available"))
	case apperror.CodeEngineModelNotAllowed:
		zh, en = fmt.Sprintf("engine=%q 不接受 model", p.text("engine")), fmt.Sprintf("engine=%q does not accept model", p.text("engine"))
	case apperror.CodeEngineEffortFieldNotAllowed:
		zh = fmt.Sprintf("engine=%q 不接受 effort", p.text("engine"))
		en = fmt.Sprintf("engine=%q does not accept effort", p.text("engine"))
	case apperror.CodeEngineEffortValueNotAllowed:
		zh = fmt.Sprintf("%q 不在 engine=%q 允许集 [%s] 内", p.text("value"), p.text("engine"), p.text("allowed"))
		en = fmt.Sprintf("%q is not in the allowed set [%s] for engine=%q", p.text("value"), p.text("allowed"), p.text("engine"))
	default:
		return string(code)
	}
	return language.Select(zh, en)
}

type parameterReader struct{ params apperror.Params }

func (p parameterReader) text(name string) string {
	value := p.params[name]
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func (p parameterReader) integer(name string) int {
	switch value := p.params[name].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}
