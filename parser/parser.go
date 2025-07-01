package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/version"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/refaktor/ryegen/v2/module"
	"golang.org/x/mod/modfile"
)

type Package struct {
	Name  string
	Path  string
	Files map[string]*ast.File
}

func visitDir(
	fset *token.FileSet,
	dirPath string,
	// -1 for infinite
	depth int,
	mode parser.Mode,
	modulePath string,
	buildTags []string,
	// Called when entering a directory BEFORE onFile is called for every go file
	// If keepParsing is returned false, the contents of the directory will be skipped.
	onDir func(pkgPath string) (keepParsing bool, err error),
	// Called on every go file included in the build
	onFile func(f *ast.File, filename, pkgPath string) error,
) (goVer string, require []module.Module, err error) {
	goModPath := filepath.Join(dirPath, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		data, err := os.ReadFile(goModPath)
		if err != nil {
			return "", nil, err
		}
		mod, err := modfile.Parse(goModPath, data, nil)
		if err != nil {
			return "", nil, err
		}
		if mod.Go != nil {
			goVer = "go" + mod.Go.Version
		}
		require = make([]module.Module, len(mod.Require))
		for i, v := range mod.Require {
			require[i] = module.Module{Path: v.Mod.Path, Version: v.Mod.Version}
		}
		if modulePath != mod.Module.Mod.Path {
			return "", nil, fmt.Errorf("module path of %v does not match with module path in go.mod", modulePath)
		}
	}

	var doVisitDir func(fsPath, pkgPath string, depth int) error
	doVisitDir = func(fsPath, pkgPath string, depth int) error {
		if depth > -1 && depth == 0 {
			return nil
		}
		keepParsing, err := onDir(pkgPath)
		if err != nil {
			return err
		}
		if !keepParsing {
			return nil
		}
		ents, err := os.ReadDir(fsPath)
		if err != nil {
			return err
		}
		for _, ent := range ents {
			fsPath := filepath.Join(fsPath, ent.Name())
			if ent.IsDir() {
				if strings.HasPrefix(ent.Name(), "_") || strings.HasPrefix(ent.Name(), ".") || ent.Name() == "testdata" {
					// ignore dirs ignored by the go tool (https://pkg.go.dev/cmd/go)
					continue
				}
				if ent.Name() == "vendor" {
					// ignore non-library parts
					continue
				}
				var newModPath string
				if pkgPath != "" {
					newModPath = pkgPath + "/"
				}
				newModPath += ent.Name()
				if err := doVisitDir(fsPath, newModPath, depth-1); err != nil {
					return err
				}
			} else if strings.HasSuffix(ent.Name(), ".go") {
				if strings.HasSuffix(ent.Name(), "_test.go") {
					continue
				}
				haveBuildTag := func(tag string) bool {
					if slices.Contains(buildTags, tag) {
						return true
					}
					if strings.HasPrefix(tag, "go") {
						if version.IsValid(tag) &&
							version.IsValid(goVer) &&
							version.Compare(tag, goVer) <= 0 {
							return true
						}
					}
					return false
				}
				f, err := parser.ParseFile(fset, fsPath, nil, mode)
				if err != nil {
					return err
				}
				constr, err := fullConstraints(f, ent.Name())
				if err != nil {
					return err
				}
				if constr != nil && !constr.Eval(haveBuildTag) {
					continue
				}
				modName := f.Name.Name
				if strings.HasSuffix(modName, "_test") || modName == "main" {
					continue
				}
				if err := onFile(f, fsPath, pkgPath); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if modulePath == "std" {
		modulePath = ""
	}

	if err := doVisitDir(dirPath, modulePath, depth); err != nil {
		return "", nil, err
	}

	return goVer, require, nil
}

// ParsePackage parses a single package from a directory.
//
// packagePath is the full package path.
/*func ParsePackage(fset *token.FileSet, dirPath string, packagePath string, buildTags []string) (*Package, error) {
	var pkg *Package
	_, _, err := visitDir(
		fset,
		dirPath,
		1,
		parser.SkipObjectResolution|parser.ParseComments,
		packagePathHint,
		buildTags,
		func(pkgPath string) (keepParsing bool, err error) {
			if pkg != nil {
				panic("Package callback called twice. This is a bug.")
			}
			pkg = &Package{
				Name:  "",
				Path:  pkgPath,
				Files: make(map[string]*ast.File),
			}
			return true, nil
		},
		func(f *ast.File, filename, pkgPath string) error {
			if pkgPath != pkg.Path {
				panic("File callback called on invalid package. This is a bug.")
			}
			pkg.Name = f.Name.Name
			pkg.Files[filename] = f
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}*/

// ParseModule recursively parses a single module directory from source code.
//
// modulePath is the full module path.
// depth is the maximum depth (-1 for infinite), 1 for only current dir etc.
// goVer is the minimum go version specified in go.mod (e.g. "go1.23") or empty string if not specified.
// pkgs maps package path to [Package].
// require lists all dependencies in go.mod. If no go.mod is present, require is nil.
func ParseModule(fset *token.FileSet, dirPath string, modulePath string, depth int, buildTags []string) (goVer string, pkgs map[string]*Package, require []module.Module, err error) {
	pkgs = make(map[string]*Package)
	goVer, require, err = visitDir(
		fset,
		dirPath,
		depth,
		parser.SkipObjectResolution|parser.ParseComments,
		modulePath,
		buildTags,
		func(pkgPath string) (keepParsing bool, err error) {
			if _, ok := pkgs[pkgPath]; ok {
				return true, fmt.Errorf("duplicate package %v", pkgPath)
			}
			pkgs[pkgPath] = &Package{
				Name:  "",
				Path:  pkgPath,
				Files: make(map[string]*ast.File),
			}
			return true, nil
		},
		func(f *ast.File, filename, pkgPath string) error {
			pkg, ok := pkgs[pkgPath]
			if !ok {
				return fmt.Errorf("expected package %v to exist", pkgPath)
			}
			pkg.Name = f.Name.Name
			pkg.Files[filename] = f
			return nil
		},
	)
	if err != nil {
		return "", nil, nil, err
	}

	for pkgPath, pkg := range pkgs {
		// Remove packages with no go files
		if pkg.Name == "" {
			delete(pkgs, pkgPath)
		}
	}

	return goVer, pkgs, require, nil
}
