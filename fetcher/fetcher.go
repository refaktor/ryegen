package fetcher

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/version"
	"maps"
	"slices"

	"github.com/refaktor/ryegen/v2/module"
)

type Options struct {
	// Where to store the cache (opaque structure).
	CacheFilePath string
	// Callback for before a module starts downloading.
	OnDownloadModule func(module.Module)
}

type Result struct {
	// Covers all files in Packages
	// and Go std library.
	*token.FileSet
	// Go version fetched (minimum
	// version required by modules).
	GoVersion string
	// Module build list after MVS.
	// Contains parsed module AST.
	RequiredModules []Module
	// All packages of all RequiredModules as
	// a single map.
	Packages map[string][]*ast.File // package path -> package ASTs
}

type Module struct {
	module.Module
	// Packages contained in this module.
	Packages map[string][]*ast.File // package path -> package ASTs
}

// Fetch fetches the specified seed modules and any
// API dependencies (meaning function bodies are ignored).
// It then applies an MVS-like version selection algorithm
// to determine which packages are required for an API binding.
func Fetch(srcDir string, seeds []module.Module, opts Options, buildTags []string) (*Result, error) {
	fet := newFetcher(srcDir, opts, buildTags)
	for _, seed := range seeds {
		if err := fet.preFetchModule(seed); err != nil {
			return nil, err
		}
	}
	for _, seed := range seeds {
		if err := fet.postFetchModule(seed, &[]module.Module{}); err != nil {
			return nil, err
		}
	}

	buildList, err := fet.buildList(seeds)
	if err != nil {
		return nil, fmt.Errorf("get build list: %w", err)
	}

	goVersion := "go1"
	for _, mod := range buildList {
		v := fet.preFetched[mod].goVersion
		if version.Compare(goVersion, v) < 0 {
			goVersion = v
		}
	}

	if err := fet.fetchStdlib(goVersion); err != nil {
		return nil, err
	}
	buildList = append(buildList, module.NewModule("std", goVersion))

	requiredModules := make([]Module, len(buildList))
	for i := range requiredModules {
		parsed := fet.parsed[buildList[i]]

		packages := map[string][]*ast.File{}
		for pkgPath, pkg := range parsed {
			packages[pkgPath] = slices.Collect(maps.Values(pkg.Files))
		}
		requiredModules[i] = Module{
			Module:   buildList[i],
			Packages: packages,
		}
	}

	res := &Result{
		GoVersion:       goVersion,
		FileSet:         fet.fset,
		RequiredModules: requiredModules,
		Packages:        map[string][]*ast.File{},
	}
	for _, mod := range res.RequiredModules {
		maps.Copy(res.Packages, mod.Packages)
	}
	return res, nil
}
