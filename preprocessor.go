package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

type visitFn func(node ast.Node)

func (fn visitFn) Visit(node ast.Node) ast.Visitor {
	fn(node)
	return fn
}

// preprocess reduces the AST in ways, which remove information not
// necessary for binding generation. It does the following:
//   - Remove function parameter names.
//   - Remove all unneeded function bodies.
//   - Remove all unneeded variable declarations.
//   - Remove all unneeded imports, including those no longer
//     needed due to previous AST reduction.
//
// Passing an unpopulated fset may lead to a nil pointer dereference!
// getDefaultPackageName should return the default import name of the
// given package.
func preprocess(fset *token.FileSet, f *ast.File, getDefaultPackageName func(path string) (string, error)) error {
	// Removes function parameter and result names
	removeFieldNames := func(list []*ast.Field) (newList []*ast.Field) {
		for _, item := range list {
			typeOnly := &ast.Field{
				Doc:     item.Doc,
				Type:    item.Type,
				Tag:     item.Tag,
				Comment: item.Comment,
			}
			n := 1
			if item.Names != nil {
				n = len(item.Names)
			}
			for range n {
				newList = append(newList, typeOnly)
			}
		}
		return
	}

	// Remove all function bodies
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Name.Name == "init" {
				decl.Body.List = nil
			} else if decl.Type.TypeParams == nil {
				// TODO: Generic funcs require a body. We should still
				// find some way to make them not import anything
				// yet remain valid. Maybe just generate a body
				// with zeroed return values??

				decl.Body = nil

				if decl.Recv != nil {
					decl.Recv.List = removeFieldNames(decl.Recv.List)
				}
				decl.Type.Params.List = removeFieldNames(decl.Type.Params.List)
				if decl.Type.Results != nil {
					decl.Type.Results.List = removeFieldNames(decl.Type.Results.List)
				}
			}
		case *ast.GenDecl:
			if decl.Tok == token.VAR {
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.ValueSpec); ok && spec.Type != nil {
						// A ValueSpec must have a type or a value. We can only remove
						// the value if a type is specified.
						spec.Values = nil
					}
				}
			} else if decl.Tok == token.TYPE {
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.TypeSpec); ok {
						if iface, ok := spec.Type.(*ast.InterfaceType); ok {
							for _, m := range iface.Methods.List {
								if ft, ok := m.Type.(*ast.FuncType); ok {
									ft.Params.List = removeFieldNames(ft.Params.List)
									if ft.Results != nil {
										ft.Results.List = removeFieldNames(ft.Results.List)
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

	importsByName := map[string]importSpec{}
	for _, imp := range f.Imports {
		if imp.Name != nil && imp.Name.Name == "_" {
			continue
		}
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		name := ""
		var actualName string
		if imp.Name == nil {
			var err error
			actualName, err = getDefaultPackageName(path)
			if err != nil {
				return err
			}
		} else {
			name = imp.Name.Name
			actualName = imp.Name.Name
		}
		if actualName == "" {
			return fmt.Errorf("empty import name for %v", path)
		}
		if _, ok := importsByName[actualName]; ok {
			return fmt.Errorf("duplicate import name %v", actualName)
		}
		importsByName[actualName] = importSpec{name, path}
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
