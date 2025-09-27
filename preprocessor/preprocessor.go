package preprocessor

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

type visitFn func(node ast.Node)

func (fn visitFn) Visit(node ast.Node) ast.Visitor {
	fn(node)
	return fn
}

// Preprocess reduces the AST in ways, which remove information not
// necessary for binding generation. It does the following:
//   - Remove function parameter names.
//   - Remove all unneeded function bodies.
//   - Remove all unneeded variable declarations.
//   - Remove most unneeded imports, including those no longer
//     needed due to previous AST reduction (imports as "."
//     cannot be removed, as there isn't enough semantic context
//     in the package AST).
//
// Passing an unpopulated fset may lead to a nil pointer dereference!
// getDefaultPackageName should return the default import name of the
// given package.
func Preprocess(fset *token.FileSet, f *ast.File, getDefaultPackageName func(path string) (string, error)) error {
	// Simplifies (removes or renames to "_") function parameter and result names
	simplifyFieldNames := func(list []*ast.Field, renameToUnderscore bool) (newList []*ast.Field) {
		for _, item := range list {
			field := &ast.Field{
				Doc:     item.Doc,
				Type:    item.Type,
				Tag:     item.Tag,
				Comment: item.Comment,
			}
			if renameToUnderscore {
				field.Names = []*ast.Ident{{Name: "_"}}
			}
			n := 1
			if item.Names != nil {
				n = len(item.Names)
			}
			for range n {
				newList = append(newList, field)
			}
		}
		return
	}

	// Remove all function bodies
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv != nil {
				decl.Recv.List = simplifyFieldNames(decl.Recv.List, false)
			}
			decl.Type.Params.List = simplifyFieldNames(decl.Type.Params.List, false)
			if decl.Type.Results != nil {
				decl.Type.Results.List = simplifyFieldNames(decl.Type.Results.List, true)
				decl.Body = &ast.BlockStmt{
					List: []ast.Stmt{&ast.ReturnStmt{}},
				}
			} else {
				decl.Body = &ast.BlockStmt{}
			}
		case *ast.GenDecl:
			switch decl.Tok {
			case token.VAR:
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.ValueSpec); ok && spec.Type != nil {
						// A ValueSpec must have a type or a value. We can only remove
						// the value if a type is specified.
						spec.Values = nil
					}
				}
			case token.TYPE:
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.TypeSpec); ok {
						if iface, ok := spec.Type.(*ast.InterfaceType); ok {
							for _, m := range iface.Methods.List {
								if ft, ok := m.Type.(*ast.FuncType); ok {
									ft.Params.List = simplifyFieldNames(ft.Params.List, false)
									if ft.Results != nil {
										ft.Results.List = simplifyFieldNames(ft.Results.List, false)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	type importSpec struct {
		name string
		path string
	}

	importsAsUnderscore := map[importSpec]struct{}{}
	importsByName := map[string]importSpec{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		name := ""
		var resolvedName string
		if imp.Name == nil {
			var err error
			resolvedName, err = getDefaultPackageName(path)
			if err != nil {
				return fmt.Errorf("get import name for %v at %v: %w", path, fset.Position(imp.Pos()), err)
			}
		} else {
			name = imp.Name.Name
			resolvedName = imp.Name.Name
		}
		if resolvedName == "" {
			return fmt.Errorf("empty import name for %v", path)
		}
		switch name {
		case "_":
			importsAsUnderscore[importSpec{name, path}] = struct{}{}
		case ".":
			// We can't strip imports as ".", as we don't
			// have enough info to know if the package
			// was used.
		default:
			if _, ok := importsByName[resolvedName]; ok {
				return fmt.Errorf("duplicate import name %v", resolvedName)
			}
			importsByName[resolvedName] = importSpec{name, path}
		}
	}

	usedImports := map[importSpec]struct{}{}
	ast.Walk(visitFn(func(n ast.Node) {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return
		}
		if imp, ok := importsByName[id.Name]; ok {
			usedImports[imp] = struct{}{}
		}
	}), f)

	stripImports := map[importSpec]struct{}{}
	maps.Copy(stripImports, importsAsUnderscore)
	for _, imp := range importsByName {
		if _, used := usedImports[imp]; !used {
			stripImports[imp] = struct{}{}
		}
	}
	for imp := range stripImports {
		if !astutil.DeleteNamedImport(fset, f, imp.name, imp.path) {
			return fmt.Errorf("unable to remove import %v", strconv.Quote(imp.path))
		}
	}

	return nil
}
