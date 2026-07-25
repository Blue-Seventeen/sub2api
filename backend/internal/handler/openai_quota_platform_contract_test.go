package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRecordUsageInputsCarryQuotaPlatform(t *testing.T) {
	files := []string{
		"openai_gateway_handler.go",
		"openai_chat_completions.go",
		"openai_embeddings.go",
		"openai_images.go",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
			require.NoError(t, err)

			var missing []token.Position
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isOpenAIRecordUsageInputLiteral(literal.Type) {
					return true
				}
				if !compositeLiteralHasKey(literal, "QuotaPlatform") {
					missing = append(missing, fset.Position(literal.Lbrace))
				}
				return true
			})

			require.Empty(t, missing, "OpenAI usage post-billing must receive request-time QuotaPlatform")
		})
	}
}

func TestGatewayRecordUsageInputsCarryQuotaPlatform(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	for _, path := range files {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err)

			var missingQuotaPlatform []token.Position
			var missingChannelUsageFields []token.Position
			var missingSessionID []token.Position
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isGatewayRecordUsageInputLiteral(literal.Type) {
					return true
				}
				if !compositeLiteralHasKey(literal, "QuotaPlatform") {
					missingQuotaPlatform = append(missingQuotaPlatform, fset.Position(literal.Lbrace))
				}
				if !compositeLiteralHasKey(literal, "ChannelUsageFields") {
					missingChannelUsageFields = append(missingChannelUsageFields, fset.Position(literal.Lbrace))
				}
				if !compositeLiteralHasKey(literal, "SessionID") {
					missingSessionID = append(missingSessionID, fset.Position(literal.Lbrace))
				}
				return true
			})

			require.Empty(t, missingQuotaPlatform, "gateway usage post-billing must receive request-time QuotaPlatform")
			require.Empty(t, missingChannelUsageFields, "gateway usage post-billing must receive channel mapping fields")
			require.Empty(t, missingSessionID, "gateway usage rows must persist explicit client session identifiers")
		})
	}
}

func isOpenAIRecordUsageInputLiteral(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "service" && selector.Sel.Name == "OpenAIRecordUsageInput"
}

func isGatewayRecordUsageInputLiteral(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "service" && selector.Sel.Name == "RecordUsageInput"
}

func compositeLiteralHasKey(literal *ast.CompositeLit, key string) bool {
	for _, elt := range literal.Elts {
		pair, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := pair.Key.(*ast.Ident)
		if ok && ident.Name == key {
			return true
		}
	}
	return false
}
