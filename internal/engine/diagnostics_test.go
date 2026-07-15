package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// TestEngineErrorFormatsContainNoHan 锁定引擎适配器的技术诊断固定使用英文。
// 引擎原始返回内容是运行时数据，不在此源码字面量检查范围内，仍会原样保留。
func TestEngineErrorFormatsContainNoHan(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取 engine 包目录失败: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || !isFmtErrorf(call.Fun) {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			format, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Errorf("解析 %s 的错误格式字面量失败: %v", fileSet.Position(literal.Pos()), err)
				return true
			}
			if strings.ContainsFunc(format, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				t.Errorf("引擎技术诊断必须使用英文: %s: %q", fileSet.Position(literal.Pos()), format)
			}
			return true
		})
	}
}

func isFmtErrorf(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Errorf" {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == "fmt"
}
