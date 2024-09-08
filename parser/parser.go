package parser

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type Package struct {
	Name  string
	Path  string
	Files map[string]*ast.File
}

func visitDir(fset *token.FileSet, dirPath string, mode parser.Mode, modulePathHint string, onFile func(f *ast.File, filename, module string) error) (goVer string, require []module.Version, err error) {
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
		require = make([]module.Version, len(mod.Require))
		for i, v := range mod.Require {
			require[i] = v.Mod
		}
		modulePath = mod.Module.Mod.Path
	} else {
		noGoMod = true
		modulePath = modulePathHint
	}

	requireMap := make(map[string]struct{})

	var doVisitDir func(fsPath, modPath string) error
	doVisitDir = func(fsPath, modPath string) error {
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
				if ent.Name() == "cmd" {
					// ignore non-library parts
					continue
				}
				var newModPath string
				if modPath != "" {
					newModPath = modPath + "/"
				}
				newModPath += ent.Name()
				if err := doVisitDir(fsPath, newModPath); err != nil {
					return err
				}
			} else if strings.HasSuffix(ent.Name(), ".go") {
				if strings.HasSuffix(ent.Name(), "_test.go") {
					continue
				}
				if goos, goarch := filenameSuffixConstraints(ent.Name()); goos != "" || goarch != "" {
					continue
				}
				f, err := parser.ParseFile(fset, fsPath, nil, mode)
				if err != nil {
					return err
				}
				skip, err := func() (bool, error) {
					for _, c := range f.Comments {
						for _, c := range c.List {
							if !constraint.IsGoBuild(c.Text) {
								continue
							}
							expr, err := constraint.Parse(c.Text)
							if err != nil {
								return false, err
							}
							return !expr.Eval(func(tag string) bool {
								return false
							}), nil
						}
					}
					return false, nil
				}()
				if err != nil {
					return err
				}
				if skip {
					continue
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
				modName := f.Name.Name
				if strings.HasSuffix(modName, "_test") || modName == "main" {
					continue
				}
				if err := onFile(f, fsPath, modPath); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if noGoMod {
		require = make([]module.Version, 0, len(requireMap))
		for req := range requireMap {
			require = append(require, module.Version{Path: req})
		}
	}

	if modulePath == "std" {
		modulePath = ""
	}

	if err := doVisitDir(dirPath, modulePath); err != nil {
		return "", nil, err
	}
	return goVer, require, nil
}

// ParseDirModules fetches package info from source code.
// It recursively parses a single package directory.
//
// modulePathHint is the full package path (required if no go.mod is present).
// goVer is the semantic version of the module.
// modules maps package path to package name.
// require lists all dependencies of the parsed package.
func ParseDirModules(fset *token.FileSet, dirPath, modulePathHint string) (goVer string, modules map[string]string, require []module.Version, err error) {
	modules = make(map[string]string)
	goVer, require, err = visitDir(fset, dirPath, parser.PackageClauseOnly|parser.ImportsOnly|parser.ParseComments, modulePathHint, func(f *ast.File, filename, module string) error {
		if name, ok := modules[module]; ok && name != f.Name.Name {
			return fmt.Errorf("module %v has conflicting names: %v and %v", module, name, f.Name.Name)
		}
		modules[module] = f.Name.Name
		return nil
	})
	if err != nil {
		return "", nil, nil, err
	}

	return goVer, modules, require, nil
}

// ParseDir recursively parses a single package directory from source code.
//
// modulePathHint is the full package path (required if no go.mod is present).
// pkgs maps package path to [Package].
func ParseDir(fset *token.FileSet, dirPath string, modulePathHint string) (pkgs map[string]*Package, err error) {
	pkgs = make(map[string]*Package)
	_, _, err = visitDir(fset, dirPath, parser.SkipObjectResolution|parser.ParseComments, modulePathHint, func(f *ast.File, filename, module string) error {
		pkg, ok := pkgs[module]
		if !ok {
			pkg = &Package{
				Name:  f.Name.Name,
				Path:  module,
				Files: make(map[string]*ast.File),
			}
			pkgs[module] = pkg
		}
		pkg.Files[filename] = f
		return nil
	})
	if err != nil {
		return nil, err
	}

	return pkgs, nil
}

var (
	goosSuffixes   = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "windows", "zos"}
	goarchSuffixes = []string{"386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe", "loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le", "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"}
)

func filenameSuffixConstraints(filename string) (goosConstraint, goarchConstraint string) {
	for _, goos := range goosSuffixes {
		if strings.HasSuffix(filename, "_"+goos+".go") {
			return goos, ""
		}
	}
	for _, goarch := range goarchSuffixes {
		if strings.HasSuffix(filename, "_"+goarch+".go") {
			for _, goos := range goosSuffixes {
				if strings.HasSuffix(filename, "_"+goos+"_"+goarch+".go") {
					return goos, goarch
				}
			}
			return "", goarch
		}
	}
	return "", ""
}
