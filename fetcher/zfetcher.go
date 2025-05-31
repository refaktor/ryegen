package fetcher

import (
	"fmt"
	"go/token"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/refaktor/ryegen/v2/module"
	"github.com/refaktor/ryegen/v2/parser"
	"github.com/refaktor/ryegen/v2/preprocessor"
	"github.com/refaktor/ryegen/v2/repo"
	"golang.org/x/mod/semver"
)

type ImportCycleError struct {
	Mod   module.Module
	Stack []module.Module
}

func (err *ImportCycleError) Error() string {
	var msg strings.Builder
	msg.WriteString("import cycle detected: ")
	for _, item := range err.Stack {
		msg.WriteString(item.String())
		msg.WriteString(" imports ")
	}
	msg.WriteString(err.Mod.String())
	return msg.String()
}

// detectImportCycle examines a module call stack as generated
// by [fetcher.postFetchModule] and checks for any problematic import cycles.
// If found is false, there is no import cycle.
// If found is true and err is nil, the caller should return without an error.
// If found is true and err is non-nil, the caller should return the error.
func detectImportCycle(mod module.Module, stack *[]module.Module) (found bool, err error) {
	if idx := slices.Index(*stack, mod); idx != -1 {
		importCycle := (*stack)[idx:]
		if slices.ContainsFunc(importCycle, func(m module.Module) bool {
			return strings.HasPrefix(mod.Path, "golang.org/x/")
		}) {
			// The golang.org/x/ library collection seems to have a lot of issues with import cycles,
			// so we just ignore those here.
			// TODO: See if it's reasonable to not err on import cycle.
			return true, nil
		} else {
			return true, &ImportCycleError{Mod: mod, Stack: importCycle}
		}
	}
	return false, nil
}

type preFetchedModule struct {
	goVersion    string
	goModRequire []module.Module
	pkgNames     map[string]string
}

// findReferencedPkg finds the module referenced by the given package path witin
// the context of parent.
// Only works if the parent module was pre-fetched.
// found indicates whether the module was found.
func (fet *fetcher) findReferencedModule(parent module.Module, pkgPath string) (_ module.Module, found bool) {
	prf, ok := fet.preFetched[parent]
	if !ok {
		panic("findReferencedModule called on module that hasn't been pre-fetched")
	}
	if _, ok := prf.pkgNames[pkgPath]; ok {
		return parent, true
	}
	pof, pofOK := fet.postFetched[parent]

	for {
		for _, dep := range prf.goModRequire {
			if dep.Path == pkgPath {
				return dep, true
			}
		}
		if pofOK {
			for _, dep := range pof.fullRequireCoarse {
				if dep.Path == pkgPath {
					return dep, true
				}
			}
		}

		if idx := strings.LastIndex(pkgPath, "/"); idx <= 0 {
			break
		} else {
			pkgPath = pkgPath[:idx]
		}
	}

	return module.Module{}, false
}

type postFetchedModule struct {
	// fullRequireCoarse is like fullRequire,
	// but includes imports not pruned by
	// preprocessing.
	fullRequireCoarse []module.Module
	// fullRequire includes requirements
	// not listed in go.mod.
	fullRequire []module.Module
}

type fetcher struct {
	fset      *token.FileSet
	srcDir    string
	opts      Options
	buildTags []string // TODO: evaluate how to properly handle this

	preFetched  map[module.Module]preFetchedModule
	postFetched map[module.Module]postFetchedModule

	// parsed contains parsed module code
	// (module -> package path -> package AST).
	// Not cacheable. Already pre-processed if the
	// module is in postFetched. Otherwise not
	// pre-processed.
	parsed map[module.Module]map[string]*parser.Package

	latestModuleVersionCache map[string]string // path -> version
}

func newFetcher(srcDir string, opts Options, buildTags []string) *fetcher {
	return &fetcher{
		fset:                     token.NewFileSet(),
		srcDir:                   srcDir,
		opts:                     opts,
		buildTags:                buildTags,
		preFetched:               map[module.Module]preFetchedModule{},
		postFetched:              map[module.Module]postFetchedModule{},
		parsed:                   map[module.Module]map[string]*parser.Package{},
		latestModuleVersionCache: map[string]string{},
	}
}

// maybeDownloadModule will download the module over the network if not already
// present in srcDir.
// newlyDownloaded is true exactly when the module had to be downloaded over the
// network (i.e. wasn't already present).
// destSubdir is the sub-directory where the downloaded module source code is present.
func (fet *fetcher) maybeDownloadModule(mod module.Module) (newlyDownloaded bool, destSubdir string, err error) {
	var rep *repo.Repo
	if mod.Path != "std" {
		var err error
		rep, err = repo.GoModule(mod.Path, mod.Version)
		if err != nil {
			return false, "", fmt.Errorf("get Go module %v: %w", mod.Path, err)
		}
	} else {
		rep = repo.GoStdlib(mod.Version)
	}

	have, err := rep.Have(fet.srcDir)
	if err != nil {
		return false, "", fmt.Errorf("check module %v: %w", mod, err)
	} else if have {
		return false, rep.DestSubdir, nil
	}
	if !have {
		if fet.opts.OnDownloadModule != nil {
			fet.opts.OnDownloadModule(mod)
		}
		if err := rep.Get(fet.srcDir); err != nil {
			return false, "", fmt.Errorf("get module %v: %w", mod, err)
		}
	}

	return true, rep.DestSubdir, nil
}

func (fet *fetcher) preFetchModule(mod module.Module) error {
	newlyDownloaded, destSubdir, err := fet.maybeDownloadModule(mod)
	if err != nil {
		return fmt.Errorf("download %v: %w", mod, err)
	}

	if !newlyDownloaded {
		if _, ok := fet.preFetched[mod]; ok {
			// already fetched
			return nil
		}
	}

	goVer, pkgs, goModRequire, err := parser.ParseModule(
		fet.fset,
		filepath.Join(fet.srcDir, destSubdir),
		mod.Path,
		-1,
		fet.buildTags,
	)
	if err != nil {
		return fmt.Errorf("parse packages for %v: %w", mod, err)
	}

	pkgNames := map[string]string{}
	for _, pkg := range pkgs {
		pkgNames[pkg.Path] = pkg.Name
	}

	fet.preFetched[mod] = preFetchedModule{
		goVersion:    goVer,
		goModRequire: goModRequire,
		pkgNames:     pkgNames,
	}

	fet.parsed[mod] = pkgs

	return nil
}

// replaceEmptyVersionsWithLatest replaces empty modules' versions
// with the latest one. May use the network to get the latest version.
// Latest module versions are cached, so subsequent calls with overlapping
// modules will be much faster.
func (fet *fetcher) replaceEmptyVersionsWithLatest(mods []module.Module) error {
	for i, mod := range mods {
		if mod.Version != "" {
			continue
		}

		latest, cached := fet.latestModuleVersionCache[mod.Path]
		if !cached {
			var err error
			latest, err = repo.GoModuleGetLatestVersion(mod.Path)
			if err != nil {
				return fmt.Errorf("get latest version of %v: %w", mod.Path, err)
			}
			fet.latestModuleVersionCache[mod.Path] = latest
		}
		mods[i].Version = latest
	}
	return nil
}

// getModuleImports gets all modules actually imported by mod.
// This includes modules not referenced in go.mod (or all imported
// modules if no go.mod exists). It also resolves unspecified (empty)
// module versions. This function may use the network.
func (fet *fetcher) getModuleImports(mod module.Module) ([]module.Module, error) {
	parsed, ok := fet.parsed[mod]
	if !ok {
		return nil, fmt.Errorf("programmer error: getModuleImports called on unparsed module %v", mod)
	}

	require := map[module.Module]struct{}{}
	for _, pkg := range parsed {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				impPos := fet.fset.Position(imp.Pos())
				impPath, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return nil, fmt.Errorf("unquote import at %v: %w", impPos, err)
				}
				impMod, ok := fet.findReferencedModule(mod, impPath)
				if ok {
					if impMod != mod {
						require[impMod] = struct{}{}
					}
				} else {
					if strings.HasPrefix(impPath, "golang.org/x/") {
						// golang.org/x modules cause problems where the dependency on
						// them isn't declared correctly sometimes.

						// golang.org/x package's module path
						goXModPath := strings.Join(strings.SplitN(impPath, "/", 4)[:3], "/")

						if goXModPath != mod.Path {
							// Just use an empty version, which will get resolved to
							// the latest version.
							require[module.NewModule(goXModPath, "")] = struct{}{}
						}
					} else if !module.IsPkgPathStd(impPath) {
						return nil, fmt.Errorf("package %v imported at %v not specified in %v/go.mod", impPath, impPos, mod.Path)
					}
				}
			}
		}
	}

	deps := slices.Collect(maps.Keys(require))
	if err := fet.replaceEmptyVersionsWithLatest(deps); err != nil {
		return nil, err
	}

	return deps, nil
}

// preprocess runs the preprocessor on the module's parsed
// files, leaving only the API-relevant parts of the AST.
func (fet *fetcher) preprocess(mod module.Module) error {
	parsed, ok := fet.parsed[mod]
	if !ok {
		return fmt.Errorf("programmer error: preprocess called on unparsed module %v", mod)
	}

	for _, pkg := range parsed {
		for _, f := range pkg.Files {
			if err := preprocessor.Preprocess(fet.fset, f, func(path string) (string, error) {
				if module.IsPkgPathStd(path) || strings.HasPrefix(path, "golang.org/x/") {
					// TODO: See if guessing std and golang.org/x
					// package names is always valid.
					// I'm relatively certain it is,
					// though.
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
					lastSlash := strings.LastIndex(strippedPath, "/")
					if lastSlash == -1 {
						return strippedPath, nil
					} else {
						return strippedPath[lastSlash+1:], nil
					}
				}
				refMod, ok := fet.findReferencedModule(mod, path)
				if !ok {
					return "", fmt.Errorf("unable to find module for package %v", path)
				}
				refModInfo, ok := fet.preFetched[refMod]
				if !ok {
					return "", fmt.Errorf("programmer error: preprocess called on module %v without pre-fetching dependencies", mod)
				}
				name, ok := refModInfo.pkgNames[path]
				if !ok {
					return "", fmt.Errorf("unable to find package %v referenced by %v", pkg.Path, mod)
				}
				return name, nil
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (fet *fetcher) postFetchModule(mod module.Module, stack *[]module.Module) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("post-fetch %v: %w", mod, err)
		}
	}()

	if cycle, err := detectImportCycle(mod, stack); cycle {
		return err
	}
	*stack = append(*stack, mod)
	defer func() {
		*stack = (*stack)[:len(*stack)-1]
	}()

	if _, ok := fet.postFetched[mod]; ok {
		// already fully fetched
		return nil
	}

	var pof postFetchedModule

	{
		imps, err := fet.getModuleImports(mod)
		if err != nil {
			return err
		}
		for _, imp := range imps {
			if err := fet.preFetchModule(imp); err != nil {
				return fmt.Errorf("pre-fetch: %w", err)
			}
		}
		pof.fullRequireCoarse = imps
		// required for preprocess to find
		// dependencies
		fet.postFetched[mod] = pof
	}

	if err := fet.preprocess(mod); err != nil {
		return fmt.Errorf("preprocess: %w", err)
	}

	{
		// imports should have significantly reduced due to preprocessing
		// compared to coarse imports
		imps, err := fet.getModuleImports(mod)
		if err != nil {
			return err
		}
		// fully fetch those reduced imports
		for _, imp := range imps {
			if err := fet.postFetchModule(imp, stack); err != nil {
				return err
			}
		}

		pof.fullRequire = imps
		fet.postFetched[mod] = pof
	}

	return nil
}

func (fet *fetcher) fetchStdlib(version string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("fetch std@%v: %w", version, err)
		}
	}()

	rep := repo.GoStdlib(version)
	have, err := rep.Have(fet.srcDir)
	if err != nil {
		return err
	}
	if !have {
		fet.opts.OnDownloadModule(module.NewModule("std", version))
		err := rep.Get(fet.srcDir)
		if err != nil {
			return err
		}
	}
	goVersionNoPfx, ok := strings.CutPrefix(version, "go")
	if !ok {
		return fmt.Errorf("expected stdlib version to have \"go\" prefix, but got \"%v\"", version)
	}
	_, pkgs, _, err := parser.ParseModule(fet.fset, path.Join(fet.srcDir, "go@"+goVersionNoPfx), "std", -1, fet.buildTags)
	mod := module.NewModule("std", version)
	pkgNames := map[string]string{}
	for pkgPath, pkg := range pkgs {
		pkgNames[pkgPath] = pkg.Name
	}
	fet.preFetched[mod] = preFetchedModule{
		goVersion:    version,
		goModRequire: nil,
		pkgNames:     pkgNames,
	}
	fet.parsed[mod] = map[string]*parser.Package{}
	for _, pkg := range pkgs {
		fet.parsed[mod][pkg.Path] = pkg
	}
	if err := fet.preprocess(mod); err != nil {
		return fmt.Errorf("preprocess: %w", err)
	}

	return nil
}

// buildList returns the list of required modules after running an MVS algorithm.
// Expects all modules to be fully fetched with dependencies.
func (fet *fetcher) buildList(seeds []module.Module) ([]module.Module, error) {
	var (
		open     = map[module.Module]struct{}{}
		open1    = map[module.Module]struct{}{}
		closed   = map[module.Module]struct{}{}
		selected = map[string]string{} // path -> version
	)

	for _, seed := range seeds {
		open[seed] = struct{}{}
	}

	for len(open) != 0 {
		for n := range open {
			info, ok := fet.postFetched[n]
			if !ok {
				return nil, fmt.Errorf("programmer error: buildList called with dependency module %v not fully fetched", n)
			}
			for _, req := range info.fullRequire {
				if _, ok := closed[req]; !ok {
					open1[req] = struct{}{}
				}
			}
			if semver.Compare(selected[n.Path], n.Version) < 0 {
				selected[n.Path] = n.Version
			}
		}
		for n := range open {
			closed[n] = struct{}{}
		}
		open, open1 = open1, open
		clear(open1)
	}

	res := make([]module.Module, 0, len(selected))
	for path, version := range selected {
		res = append(res, module.NewModule(path, version))
	}
	return res, nil
}
