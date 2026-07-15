package workflow

import (
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
)

// RenameNodeID 把 def 中 oldID 的 agent 节点改名为 newID，并级联同步所有边端点与各节点模板里的
// {{oldID}} 引用（转义 \{{oldID}} 视作字面量、不动），使改 id 后不留悬空引用。校验：newID 须合法、
// 非保留标记名、未被占用；oldID 须为存在的 agent 节点。任一不过返回 error 且不改动 def。newID == oldID
// 视作空操作返回 nil。就地改 def.Nodes / def.Edges、不重建切片，故外部持有的节点指针仍有效。
//
// 这是内核历来把「改 id」归为全量 edit（因 id 有引用完整性）的破题：改名连带改引用一次做完，
// 供 CLI `node set --id` 与 UI 检查器 id 字段共用同一套引用完整性语义。
func RenameNodeID(def *Definition, oldID, newID string) error {
	if newID == oldID {
		return nil
	}
	if newID == NodeIDStart || newID == NodeIDEnd {
		return apperror.New(apperror.CodeReservedNodeID, apperror.Params{"id": newID})
	}
	if !IsValidNodeID(newID) {
		return apperror.New(apperror.CodeInvalidNodeID, apperror.Params{"id": newID})
	}
	found := false
	for i := range def.Nodes {
		switch def.Nodes[i].ID {
		case newID:
			return apperror.New(apperror.CodeNodeAlreadyExists, apperror.Params{"id": newID})
		case oldID:
			if def.Nodes[i].IsMarker() {
				return apperror.New(apperror.CodeReservedNodeID, apperror.Params{"id": oldID, "action": "rename"})
			}
			found = true
		}
	}
	if !found {
		return apperror.New(apperror.CodeNodeNotFound, apperror.Params{"id": oldID})
	}

	for i := range def.Nodes {
		if def.Nodes[i].ID == oldID {
			def.Nodes[i].ID = newID
		}
		def.Nodes[i].PromptTemplate = renameTemplateReference(def.Nodes[i].PromptTemplate, oldID, newID)
	}
	for i := range def.Edges {
		if def.Edges[i].From == oldID {
			def.Edges[i].From = newID
		}
		if def.Edges[i].To == oldID {
			def.Edges[i].To = newID
		}
	}
	return nil
}

// renameTemplateReference 把模板里的活引用 {{oldKey}} 改为 {{newKey}}；转义 \{{oldKey}} 是字面量、不动。
// 与 render.go / validate.go 的 templateVariablePattern 严格同源。
func renameTemplateReference(template, oldKey, newKey string) string {
	return templateVariablePattern.ReplaceAllStringFunc(template, func(matched string) string {
		if strings.HasPrefix(matched, "\\") {
			return matched // 转义字面量不动
		}
		if templateVariablePattern.FindStringSubmatch(matched)[1] == oldKey {
			return "{{" + newKey + "}}"
		}
		return matched
	})
}
