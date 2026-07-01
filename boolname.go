// Package boolname provides a go/analysis analyzer enforcing the gomatic Go
// boolean naming standard: boolean identifiers carry an is/has/can/should/will
// predicate prefix, or an Enabled/Disabled flag suffix. For parameters and
// named results it offers a mechanical is-prefix rename fix.
package boolname

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	goyze "github.com/gomatic/go-yze"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var prefixes = []string{"is", "has", "can", "should", "will"}

// message is the diagnostic format; its one verb is the ill-named identifier.
const message = "boolean %s should use an is/has/can/should/will prefix or an Enabled/Disabled suffix"

// Analyzer reports boolean fields, parameters, and results that are not named as
// predicates or flags.
var Analyzer = &analysis.Analyzer{
	Name:     "boolname",
	Doc:      "reports boolean identifiers lacking an is/has/can/should/will prefix or an Enabled/Disabled suffix",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// Registration declares this analyzer to the yze framework.
var Registration = goyze.Registration{
	Name:       "boolname",
	Categories: []goyze.Category{"naming"},
	URL:        "https://docs.gomatic.dev/yze/boolname",
	Analyzer:   Analyzer,
}

// run reports each ill-named boolean field, parameter, and named result. Only
// signature names (parameters and results) are fixable: a struct-field rename
// could break references in _test.go files or other packages, which the yze
// driver does not load.
func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.StructType)(nil), (*ast.FuncType)(nil)}, func(n ast.Node) {
		_, isStruct := n.(*ast.StructType)
		for _, field := range fieldsOf(n) {
			for _, name := range field.Names {
				checkName(pass, name, !isStruct)
			}
		}
	})
	return nil, nil
}

// fieldsOf returns the fields a node contributes: a struct's fields, or a
// function signature's parameters and results.
func fieldsOf(n ast.Node) []*ast.Field {
	if st, ok := n.(*ast.StructType); ok {
		return st.Fields.List
	}
	ft := n.(*ast.FuncType)
	return append(listOf(ft.Params), listOf(ft.Results)...)
}

func listOf(fields *ast.FieldList) []*ast.Field {
	if fields == nil {
		return nil
	}
	return fields.List
}

// checkName reports name when it is boolean but not predicate- or flag-named,
// attaching a rename fix when isFixable and the rename is provably safe. The
// blank identifier carries no name to constrain and is skipped.
func checkName(pass *analysis.Pass, name *ast.Ident, isFixable bool) {
	if name.Name == "_" {
		return
	}
	if !isBoolean(pass, name) || wellNamed(name.Name) {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:            name.Pos(),
		End:            name.End(),
		Message:        fmt.Sprintf(message, name.Name),
		SuggestedFixes: fixesFor(pass, name, isFixable),
	})
}

// fixesFor returns the deterministic rename fix ("is" + upper-cased first rune,
// so unexported-ness is always preserved), or nil when renaming is not provably
// safe. Signature names are safe to rename because Go makes them referenceable
// only from their own signature scope and function body — never from a _test.go
// file or another package — and that includes bodyless signatures (interface
// methods, func-type fields and variables), whose names have no references at
// all. Exported-looking names are outside the heuristic's lowercase domain and
// a proposed name already visible in, enclosing, or nested within the signature
// scope is a collision; both keep the diagnostic fix-free.
func fixesFor(pass *analysis.Pass, name *ast.Ident, isFixable bool) []analysis.SuggestedFix {
	if !isFixable || token.IsExported(name.Name) {
		return nil
	}
	proposed := "is" + upperFirst(name.Name)
	obj := pass.TypesInfo.Defs[name]
	if collides(obj.Parent(), proposed) {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message:   fmt.Sprintf("rename %s to %s", name.Name, proposed),
		TextEdits: renameEdits(pass, obj, proposed),
	}}
}

// upperFirst upcases name's first rune, decoding it (rather than the lead byte)
// so a multi-byte initial such as the é of "état" round-trips correctly.
func upperFirst(name string) string {
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// collides reports whether proposed is already declared in the signature scope
// or any scope enclosing it (function-body locals share the signature scope;
// file and package scopes enclose it), or in any scope nested within it, where
// the renamed identifier would be shadowed.
func collides(scope *types.Scope, proposed string) bool {
	if _, obj := scope.LookupParent(proposed, token.NoPos); obj != nil {
		return true
	}
	return declaredWithin(scope, proposed)
}

// declaredWithin reports whether name is declared in any scope nested below scope.
func declaredWithin(scope *types.Scope, name string) bool {
	for i := range scope.NumChildren() {
		child := scope.Child(i)
		if child.Lookup(name) != nil || declaredWithin(child, name) {
			return true
		}
	}
	return false
}

// renameEdits rewrites obj's declaration and every reference to proposed.
// Signature names are only referenceable from their own signature and body, so
// the declaring file contains every ident that resolves to obj.
func renameEdits(pass *analysis.Pass, obj types.Object, proposed string) []analysis.TextEdit {
	var edits []analysis.TextEdit
	ast.Inspect(fileOf(pass, obj.Pos()), func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && resolvesTo(pass, id, obj) {
			edits = append(edits, analysis.TextEdit{Pos: id.Pos(), End: id.End(), NewText: []byte(proposed)})
		}
		return true
	})
	return edits
}

// resolvesTo reports whether id declares or references obj.
func resolvesTo(pass *analysis.Pass, id *ast.Ident, obj types.Object) bool {
	return pass.TypesInfo.Defs[id] == obj || pass.TypesInfo.Uses[id] == obj
}

// fileOf returns the file containing pos. Every reported ident comes from a
// file in pass.Files, so the lookup always succeeds.
func fileOf(pass *analysis.Pass, pos token.Pos) *ast.File {
	return pass.Files[slices.IndexFunc(pass.Files, func(file *ast.File) bool {
		return file.FileStart <= pos && pos < file.FileEnd
	})]
}

// isBoolean reports whether name's defined object has a boolean underlying type.
// name is a non-blank field, parameter, or result identifier, which always has a
// defined object.
func isBoolean(pass *analysis.Pass, name *ast.Ident) bool {
	basic, ok := pass.TypesInfo.Defs[name].Type().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Bool
}

func wellNamed(name string) bool {
	return hasPredicatePrefix(name) || hasFlagSuffix(name)
}

func hasPredicatePrefix(name string) bool {
	for _, prefix := range prefixes {
		if matchesPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// matchesPrefix reports whether name begins with prefix at a word boundary.
func matchesPrefix(name, prefix string) bool {
	if !strings.HasPrefix(strings.ToLower(name), prefix) {
		return false
	}
	rest := name[len(prefix):]
	return rest != "" && startsUpper(rest)
}

// startsUpper reports whether rest begins with an uppercase or titlecase rune,
// marking the word boundary that follows a predicate prefix. Decoding the first
// rune (rather than the lead byte) admits non-ASCII boundaries such as "État".
func startsUpper(rest string) bool {
	r, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsUpper(r) || unicode.IsTitle(r)
}

func hasFlagSuffix(name string) bool {
	return strings.HasSuffix(name, "Enabled") || strings.HasSuffix(name, "Disabled")
}
