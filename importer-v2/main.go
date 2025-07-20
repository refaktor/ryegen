package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/refaktor/ryegen/v2/module"
	"github.com/refaktor/ryegen/v2/preprocessor"
	"golang.org/x/tools/go/packages"
)

func main() {
	flag.Parse()

	setupCfg := func(cfg *packages.Config) {
		cfg.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")
		cfg.BuildFlags = append([]string{"-tags=" + "required"}, cfg.BuildFlags...)
	}
	loadPkgs := func(cfg *packages.Config, patterns ...string) []*packages.Package {
		pkgs, err := packages.Load(cfg, patterns...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load: %v\n", err)
			os.Exit(1)
		}
		packages.Visit(pkgs, nil, func(p *packages.Package) {
			// "imported and not used" errors are soft and can safely be ignored.
			p.Errors = slices.DeleteFunc(p.Errors, func(err packages.Error) bool {
				return err.Kind == packages.TypeError &&
					strings.HasSuffix(err.Msg, " imported and not used")
			})
		})
		if packages.PrintErrors(pkgs) > 0 {
			fmt.Println("exiting due to errors")
			os.Exit(1)
		}
		return pkgs
	}

	var wantPkgs []string
	{
		cfg := &packages.Config{
			Mode: packages.NeedName,
		}
		setupCfg(cfg)
		pkgs := loadPkgs(cfg, flag.Args()...)
		for _, pkg := range pkgs {
			skip := false
			for sp := range strings.SplitSeq(pkg.PkgPath, "/") {
				if sp == "internal" || sp == "cmd" {
					skip = true
					break
				}
			}
			if !skip {
				wantPkgs = append(wantPkgs, pkg.ID)
			}
		}
	}

	fmt.Println(wantPkgs)

	pkgNames := map[string]string{}
	{
		cfg := &packages.Config{
			Mode: packages.LoadImports | packages.NeedDeps,
			/*ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
				f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution|parser.ParseComments)
				if err != nil {
					return nil, err
				}
				if err := preprocessor.Preprocess(fset, f, func(path string) (string, error) {

				}); err != nil {
					return nil, err
				}
				return f, nil
			},*/
		}
		setupCfg(cfg)
		pkgs := loadPkgs(cfg, wantPkgs...)

		packages.Visit(pkgs,
			nil,
			func(p *packages.Package) { pkgNames[p.PkgPath] = p.Name })
	}

	for id, name := range pkgNames {
		fmt.Println(id, name)
		/*fmt.Println(pkg.ID, pkg.Name)
		for _, pkg := range pkg.Imports {
			fmt.Println(pkg.ID, strconv.Quote(pkg.Name))
		}*/
	}

	var mu sync.Mutex
	{
		cfg := &packages.Config{
			Mode: packages.LoadTypes | packages.NeedDeps,
			ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
				mu.Lock()
				defer mu.Unlock()

				f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution|parser.ParseComments)
				if err != nil {
					log.Fatal(err)
					return nil, err
				}
				if err := preprocessor.Preprocess(fset, f, func(path string) (string, error) {
					stripVersionSuffix := func(path string) string {
						lastSlash := strings.LastIndex(path, "/")
						if lastSlash != -1 {
							if after, ok := strings.CutPrefix(path[lastSlash+1:], "v"); ok {
								v, err := strconv.Atoi(after)
								if err == nil && v >= 2 {
									return path[:lastSlash]
								}
							}
						}
						return path
					}
					strippedPath := stripVersionSuffix(path)
					if module.IsPkgPathStd(path) || strings.HasPrefix(path, "golang.org/x/") {
						lastSlash := strings.LastIndex(strippedPath, "/")
						if lastSlash == -1 {
							return strippedPath, nil
						} else {
							return strippedPath[lastSlash+1:], nil
						}
					}
					name, ok := pkgNames[path]
					if !ok {
						return "", errors.New("package not found: " + strconv.Quote(path))
					}
					return name, nil
				}); err != nil {
					log.Fatal(err)
					return nil, err
				}
				/*if err := format.Node(os.Stdout, fset, f); err != nil {
					log.Fatal(err)
				}*/
				//os.Exit(0)
				return f, nil
			},
		}
		setupCfg(cfg)
		pkgs := loadPkgs(cfg, wantPkgs...)
		_ = pkgs

	}
}
