package loader

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/refaktor/ryegen/v2/pkgutils"
	"github.com/refaktor/ryegen/v2/preprocessor"
	"golang.org/x/tools/go/packages"
)

type Config struct {
	// Packages to load
	PackagePatterns []string
	// Additional env vars (e.g. "GOOS=...", "GOARCH=...", "CGO_ENABLED=..." etc.)
	Env []string
	// Additional build flags (e.g. "-tags=...")
	BuildFlags []string
}

func loadPackagesStep(c *Config, pc *packages.Config) ([]*packages.Package, error) {
	{
		prevEnv := pc.Env
		prevBuildFlags := pc.BuildFlags
		defer func() {
			pc.Env = prevEnv
			pc.BuildFlags = prevBuildFlags
		}()
		// NOTE: Ensure we always fully clone any slices here!
		pc.Env = append(os.Environ(), c.Env...)
		pc.BuildFlags = append(slices.Clone(c.BuildFlags), pc.BuildFlags...)
	}

	pkgs, err := packages.Load(pc, c.PackagePatterns...)
	if err != nil {
		return nil, err
	}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		// "imported and not used" errors are soft and can safely be ignored.
		p.Errors = slices.DeleteFunc(p.Errors, func(err packages.Error) bool {
			return err.Kind == packages.TypeError &&
				strings.HasSuffix(err.Msg, " imported and not used")
		})
	})
	if packages.PrintErrors(pkgs) > 0 {
		return nil, errors.New("loader had errors")
	}
	return pkgs, nil
}

// ResolvePatterns only resolves the given package patterns
// and returns the sorted base package paths.
func ResolvePatterns(c *Config) ([]string, error) {
	pkgs, err := loadPackagesStep(c, &packages.Config{
		Mode: packages.NeedName,
	})
	if err != nil {
		return nil, err
	}

	var res []string
	for _, pkg := range pkgs {
		res = append(res, pkg.PkgPath)
	}
	slices.Sort(res)
	return res, nil
}

// Load fully loads and type-checks the packages.
func Load(c *Config) ([]*packages.Package, error) {
	// Load package names (the name specified in a Go file with the "package" directive)
	pkgNames := map[string]string{}
	{
		pkgs, err := loadPackagesStep(c, &packages.Config{
			Mode: packages.LoadImports | packages.NeedDeps,
		})
		if err != nil {
			return nil, err
		}

		packages.Visit(pkgs,
			nil,
			func(p *packages.Package) { pkgNames[p.PkgPath] = p.Name })
	}

	getPackageName := func(path string) (string, error) {
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

		// It should be OK to assume that both std
		// and golang.org/x packages have their last
		// component as the package name.
		if pkgutils.IsPkgPathStd(path) || strings.HasPrefix(path, "golang.org/x/") {
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
	}

	return loadPackagesStep(c, &packages.Config{
		Mode: packages.LoadSyntax | packages.NeedDeps,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				return nil, err
			}
			if err := preprocessor.Preprocess(fset, f, getPackageName); err != nil {
				return nil, err
			}
			return f, nil
		},
	})
}
