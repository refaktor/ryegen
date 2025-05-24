package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	modulePathHint string,
	buildTags []string,
	// Called when entering a directory BEFORE onFile is called for every go file
	// If keepParsing is returned false, the contents of the directory will be skipped.
	onDir func(pkgPath string) (keepParsing bool, err error),
	// Called on every go file included in the build
	onFile func(f *ast.File, filename, pkgPath string) error,
) (goVer string, require []module.Module, err error) {
	noGoMod := false

	var modulePath string
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
			goVer = mod.Go.Version
		}
		require = make([]module.Module, len(mod.Require))
		for i, v := range mod.Require {
			require[i] = module.Module{v.Mod.Path, v.Mod.Version}
		}
		modulePath = mod.Module.Mod.Path
	} else {
		noGoMod = true
		modulePath = modulePathHint
	}

	requireMap := make(map[string]struct{})

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
				if ent.Name() == "cmd" || ent.Name() == "vendor" {
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
					return slices.Contains(buildTags, tag)
				}
				f, err := parser.ParseFile(fset, fsPath, nil, mode)
				if err != nil {
					return err
				}
				if noGoMod {
					for _, imp := range f.Imports {
						pkg, err := strconv.Unquote(imp.Path.Value)
						if err != nil {
							return err
						}
						if sp := strings.Split(pkg, "/"); len(sp) > 3 {
							pkg = strings.Join(sp[:3], "/")
						}
						requireMap[pkg] = struct{}{}
					}
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

	if noGoMod {
		require = make([]module.Module, 0, len(requireMap))
		for req := range requireMap {
			require = append(require, module.Module{Path: req})
		}
	}

	if modulePath == "std" {
		modulePath = ""
	}

	if err := doVisitDir(dirPath, modulePath, depth); err != nil {
		return "", nil, err
	}
	return goVer, require, nil
}

type PackageInfo struct {
	Name    string
	Imports map[string]struct{}
}

// ParseModuleInfo parses a single module directory containing packages. It is
// faster than ParseModuleFull, but only returns superficial information.
//
// modulePathHint is the full package path (required if no go.mod is present).
// goVer is the semantic version of the module.
// pkgs maps all package paths, which aren't excluded due to build constraints or their name,
// to their name names and imports.
// require lists all dependencies of go.mod (or, if no go.mod, all possible imports).
func ParseModuleInfo(fset *token.FileSet, dirPath, modulePathHint string, buildTags []string) (goVer string, pkgs map[string]*PackageInfo, require []module.Module, err error) {
	pkgs = map[string]*PackageInfo{}

	goVer, require, err = visitDir(
		fset,
		dirPath,
		-1,
		parser.SkipObjectResolution|parser.ImportsOnly|parser.ParseComments,
		modulePathHint,
		buildTags,
		func(pkgPath string) (keepParsing bool, err error) {
			if _, ok := pkgs[pkgPath]; !ok {
				pkgs[pkgPath] = &PackageInfo{}
			}
			return true, nil
		},
		func(f *ast.File, filename, pkgPath string) error {
			if pkg, ok := pkgs[pkgPath]; ok && pkg.Name != "" && pkg.Name != f.Name.Name {
				return fmt.Errorf("package %v has conflicting names: %v and %v", pkgPath, pkg.Name, f.Name.Name)
			}
			pkgs[pkgPath].Name = f.Name.Name
			if pkgs[pkgPath].Imports == nil {
				pkgs[pkgPath].Imports = map[string]struct{}{}
			}
			for _, imp := range f.Imports {
				importedPkg, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				pkgs[pkgPath].Imports[importedPkg] = struct{}{}
			}
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

// ParsePackage parses a single package from a directory.
//
// packagePathHint is the full package path.
func ParsePackage(fset *token.FileSet, dirPath string, packagePathHint string, buildTags []string) (*Package, error) {
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
}

// ParseModuleFull recursively parses a single module directory from source code.
//
// Deprecated: Consider removing as we shouldn't need this.
//
// modulePathHint is the full package path (required if no go.mod is present).
// depth is the maximum depth (-1 for infinite), 1 for only current dir etc.
// pkgs maps package path to [Package].
// If onlyPkgs is nil, all packages in the module are parsed. If it is not
// nil, only the included packages are parsed.
func ParseModuleFull(fset *token.FileSet, dirPath string, modulePathHint string, depth int, onlyPkgs map[string]struct{}, buildTags []string) (pkgs map[string]*Package, err error) {
	pkgs = make(map[string]*Package)
	_, _, err = visitDir(
		fset,
		dirPath,
		depth,
		parser.SkipObjectResolution|parser.ParseComments,
		modulePathHint,
		buildTags,
		func(pkgPath string) (keepParsing bool, err error) {
			if onlyPkgs != nil {
				if _, ok := onlyPkgs[pkgPath]; !ok {
					return false, nil
				}
			}
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
		return nil, err
	}

	return pkgs, nil
}
