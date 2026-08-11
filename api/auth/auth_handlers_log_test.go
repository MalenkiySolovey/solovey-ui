package auth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestAuthHandlersDoNotLogCredentialIdentifiers(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "auth_handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	credentialIdentifiers := map[string]struct{}{
		"username":  {},
		"loginUser": {},
		"password":  {},
	}
	loggerCalls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok || owner.Name != "logger" {
			return true
		}
		loggerCalls++
		for _, argument := range call.Args {
			ast.Inspect(argument, func(value ast.Node) bool {
				identifier, ok := value.(*ast.Ident)
				if ok {
					if _, forbidden := credentialIdentifiers[identifier.Name]; forbidden {
						t.Errorf("logger.%s received credential identifier %q", selector.Sel.Name, identifier.Name)
					}
				}
				return true
			})
		}
		return true
	})
	if loggerCalls == 0 {
		t.Fatal("authentication handler logger calls were not inspected")
	}
}
