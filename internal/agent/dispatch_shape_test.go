package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/orchestra/orchestra/internal/toolspec"
)

// The agent's handler table and toolspec agree on which tools the agent
// answers itself: a tool marked InProcess with no handler would reach
// tools.Runner as "unknown tool", and a handler toolspec does not mark would
// be skipped by the parallel batch's in-process check.
func TestInProcessTableMatchesToolspec(t *testing.T) {
	for _, s := range toolspec.All() {
		if _, has := inProcessTools[s.Name]; has != s.InProcess {
			t.Errorf("%s: toolspec InProcess=%v, handler present=%v", s.Name, s.InProcess, has)
		}
	}
	for name := range inProcessTools {
		if _, ok := toolspec.Lookup(name); !ok {
			t.Errorf("handler for %q, which toolspec does not know", name)
		}
	}
}

// The dispatcher is a chain of gates, a table of handlers and the Runner call,
// each small enough to read. runSerialToolCall was 863 lines of `if name ==`
// with the refusal block written out 18 times.
func TestDispatchFunctionsStaySmall(t *testing.T) {
	const maxLines = 150
	fset := token.NewFileSet()
	for _, file := range []string{"tool_dispatch.go", "tool_gates.go", "inprocess_dispatch.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
			if lines > maxLines {
				t.Errorf("%s: %s is %d lines (max %d)", file, fn.Name.Name, lines, maxLines)
			}
		}
	}
}
