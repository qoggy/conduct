package message

import (
	"regexp"
	"testing"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/locale"
)

func TestEveryApplicationErrorCodeHasBilingualRendering(t *testing.T) {
	params := apperror.Params{
		"name": "demo", "id": "node", "path": "/tmp/demo", "status": "completed",
		"from": "a", "to": "b", "count": 2, "field": "engine", "kind": "Origin",
		"value": "x", "cycle": "a→b→a", "key": "sys.unknown", "engine": "demo",
		"available": "codex", "allowed": "low,high", "expectedField": "reasoningEffort",
	}
	hanzi := regexp.MustCompile(`[\p{Han}]`)
	for _, code := range apperror.AllCodes() {
		t.Run(string(code), func(t *testing.T) {
			errorValue := apperror.New(code, params)
			if code == apperror.CodeWorkflowValidationFailed {
				errorValue = apperror.Validation([]apperror.Problem{{Path: "definition.nodes", Code: apperror.CodeNodesRequired}})
			}
			chinese := Error(locale.Chinese, errorValue)
			english := Error(locale.English, errorValue)
			if chinese == string(code) || !hanzi.MatchString(chinese) {
				t.Errorf("Chinese rendering is missing: %q", chinese)
			}
			if english == string(code) || hanzi.MatchString(english) {
				t.Errorf("English rendering is missing or contains Chinese product text: %q", english)
			}
		})
	}
}

func TestReservedNodeIDRenderingMatchesAction(t *testing.T) {
	for _, test := range []struct {
		action      string
		wantChinese string
		wantEnglish string
	}{
		{action: "", wantChinese: "节点 id 不得为保留名 START", wantEnglish: "node id must not use reserved name START"},
		{action: "remove", wantChinese: "保留标记节点 START 不能删除", wantEnglish: "reserved marker node START cannot be removed"},
		{action: "rename", wantChinese: "保留标记节点 START 不能改名", wantEnglish: "reserved marker node START cannot be renamed"},
	} {
		params := apperror.Params{"id": "START"}
		if test.action != "" {
			params["action"] = test.action
		}
		err := apperror.New(apperror.CodeReservedNodeID, params)
		if got := Error(locale.Chinese, err); got != test.wantChinese {
			t.Errorf("Chinese action %q = %q, want %q", test.action, got, test.wantChinese)
		}
		if got := Error(locale.English, err); got != test.wantEnglish {
			t.Errorf("English action %q = %q, want %q", test.action, got, test.wantEnglish)
		}
	}
}
