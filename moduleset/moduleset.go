package moduleset

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	go_version "go/version"

	"github.com/refaktor/ryegen/v2/module"
	"github.com/refaktor/ryegen/v2/parser"
	"github.com/refaktor/ryegen/v2/repo"
	"golang.org/x/mod/semver"
)

type Status int

const (
	StatusDownloadStarted Status = iota
	StatusDownloadFinished
)

type Module struct {
	// Go version listed in go.mod, or "" if no go.mod.
	GoVersion string
	// All possible dependencies, i.e. those listed in go.mod,
	// or, if no go.mod is present, all modules imported by any
	// Go file.
	Dependencies []module.Module
	// All packages belonging to the module
	Packages []string
}

type ModuleSetCache struct {
	BuildTags    []string
	Modules      map[module.Module]Module
	PackageDeps  map[Package][]Package
	PackageNames map[Package]string
}

type ModuleSet struct {
	// Callback for when a package download status changes.
	OnDownload func(mod module.Module, status Status)

	BuildTags      []string
	Modules        map[module.Module]Module
	PackageDeps    map[Package][]Package
	PackageNames   map[Package]string
	GoVersion      string            // current go std version
	latestVersions map[string]string // module path to latest version (cache)
	fset           *token.FileSet
	srcDir         string
}

// Cache may be nil.
// Cache is invalidated if build tags differ from build tags in cache.
func New(srcDir string, cache *ModuleSetCache, buildTags []string) *ModuleSet {
	ms := &ModuleSet{
		BuildTags:      buildTags,
		latestVersions: map[string]string{},
		fset:           token.NewFileSet(),
		srcDir:         srcDir,
	}
	if cache == nil || !slices.Equal(buildTags, cache.BuildTags) {
		ms.Modules = map[module.Module]Module{}
		ms.PackageDeps = map[Package][]Package{}
		ms.PackageNames = map[Package]string{}
	} else {
		ms.Modules = cache.Modules
		ms.PackageDeps = cache.PackageDeps
		ms.PackageNames = cache.PackageNames
	}
	return ms
}

// Supported extensions are ".json", ".gob"
func NewWithCacheFile(srcDir, modcachePath string, buildTags []string) (*ModuleSet, error) {
	var ms *ModuleSet
	var cache *ModuleSetCache
	if data, err := os.ReadFile(modcachePath); err == nil {
		cache = &ModuleSetCache{}
		switch filepath.Ext(modcachePath) {
		case ".json":
			if err := json.Unmarshal(data, cache); err != nil {
				return nil, err
			}
		case ".gob":
			dec := gob.NewDecoder(bytes.NewReader(data))
			if err := dec.Decode(cache); err != nil {
				return nil, err
			}
		default:
			panic("unknown file extension")
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	ms = New(srcDir, cache, buildTags)
	return ms, nil
}

// Requires [ModuleSet.FetchGo] to have been in order to
// get the source dir of std library packages.
func (ms *ModuleSet) PkgSrcDir(pkgPath string) (string, error) {
	if module.IsPkgPathStd(pkgPath) {
		// std module
		goVersion := ms.RequiredGoVersion()
		return path.Join(ms.srcDir, "go@"+goVersion, pkgPath), nil
	} else {
		// non-std module
		modules := map[string]module.Module{}
		for mod := range ms.Modules {
			// This is also doing MVS... we should despaghettify.
			curr := modules[mod.Path]
			if curr.Version == "" || semver.Compare(mod.Version, curr.Version) > 0 {
				modules[mod.Path] = mod
			}
		}
		mod, ok := findPackageModuleFunc(pkgPath, func(modulePath string) (module.Module, bool) {
			if mod, ok := modules[modulePath]; ok {
				return mod, true
			}
			return module.Module{}, false
		})
		if !ok {
			return "", fmt.Errorf("unable to find module for package %v", pkgPath)
		}
		if mod.Version == "" {
			return "", fmt.Errorf("finding module for package %v: module %v has no version", pkgPath, mod)
		}
		if !strings.HasPrefix(pkgPath, mod.Path) {
			return "", fmt.Errorf("expected package %v: to have module %v prefix", pkgPath, mod)
		}
		rep, err := repo.GoModule(mod.Path, mod.Version)
		if err != nil {
			return "", fmt.Errorf("get Go module %v: %w", mod.Path, err)
		}
		return path.Join(ms.srcDir, rep.DestSubdir, strings.TrimPrefix(pkgPath, mod.Path)), nil
	}
}

func (ms *ModuleSet) Cache() ModuleSetCache {
	return ModuleSetCache{ms.BuildTags, ms.Modules, ms.PackageDeps, ms.PackageNames}
}

func (ms *ModuleSet) SaveCacheToFile(modcachePath string) error {
	var data []byte
	switch filepath.Ext(modcachePath) {
	case ".json":
		var err error
		data, err = json.MarshalIndent(ms.Cache(), "", "    ")
		if err != nil {
			return err
		}
	case ".gob":
		var b bytes.Buffer
		enc := gob.NewEncoder(&b)
		if err := enc.Encode(ms.Cache()); err != nil {
			return err
		}
		data = b.Bytes()
	default:
		panic("unknown file extension")
	}
	if err := os.WriteFile(modcachePath, data, 0666); err != nil {
		return err
	}
	return nil
}

// Finds the module corresponding to a package.
// If a module with the given path matches,
// getFunc must return true and the matching module.
func findPackageModuleFunc(pkgPath string, getFunc func(modulePath string) (module.Module, bool)) (module.Module, bool) {
	path := pkgPath

	for {
		if mod, ok := getFunc(path); ok {
			return mod, true
		}

		if idx := strings.LastIndex(path, "/"); idx <= 0 {
			break
		} else {
			path = path[:idx]
		}
	}

	return module.Module{}, false
}

func (ms *ModuleSet) fetch(mod module.Module, stack *[]module.Module) error {
	var rep *repo.Repo
	if mod.Path != "std" {
		var err error
		rep, err = repo.GoModule(mod.Path, mod.Version)
		if err != nil {
			return fmt.Errorf("get Go module %v: %w", mod.Path, err)
		}
	} else {
		rep = repo.GoStdlib(mod.Version)
	}

	//fmt.Println("fetching", mod)

	// Detect import cycle
	if idx := slices.Index(*stack, mod); idx != -1 {
		importCycle := (*stack)[idx:]
		if slices.ContainsFunc(importCycle, func(m module.Module) bool {
			return strings.HasPrefix(mod.Path, "golang.org/x/")
		}) {
			// The golang.org/x/ library collection seems to have a lot of issues with import cycles,
			// so we just ignore those here.
			// TODO: See if it's reasonable to not err on import cycle.
			return nil
		} else {
			var errmsg strings.Builder
			errmsg.WriteString("import cycle detected: ")
			for _, elem := range importCycle {
				errmsg.WriteString(elem.String())
				errmsg.WriteString(" imports ")
			}
			errmsg.WriteString(mod.String())
			return errors.New(errmsg.String())
		}
	}
	*stack = append(*stack, mod)
	defer func() {
		*stack = (*stack)[:len(*stack)-1]
	}()

	have, err := rep.Have(ms.srcDir)
	if err != nil {
		return fmt.Errorf("check module %v: %w", mod, err)
	} else if have {
		if _, ok := ms.Modules[mod]; ok { // Module already fetched
			return nil
		}
	}

	if !have {
		ms.OnDownload(mod, StatusDownloadStarted)
		if err := rep.Get(ms.srcDir); err != nil {
			return fmt.Errorf("get module %v: %w", mod, err)
		}
		ms.OnDownload(mod, StatusDownloadFinished)
	}

	goVer, pkgs, goModRequire, err := parser.ParseModuleInfo(ms.fset, filepath.Join(ms.srcDir, rep.DestSubdir), mod.Path, ms.BuildTags)
	if err != nil {
		return fmt.Errorf("parse packages for %v: %w", mod, err)
	}

	// Finds the module corresponding to an imported package.
	findPackageModule := func(pkgPath string) (module.Module, bool) {
		return findPackageModuleFunc(pkgPath, func(modulePath string) (module.Module, bool) {
			if modulePath == mod.Path {
				// Module importing from itself
				return mod, true
			}
			for _, req := range goModRequire {
				// Importing from other module
				if req.Path == modulePath {
					return req, true
				}
			}
			return module.Module{}, false
		})
	}

	// Figure out modules that are actually needed. These are a subset of "goModRequire".
	var require []module.Module
	{
		neededModules := map[module.Module]struct{}{}
		for pkgPath, pkg := range pkgs {
			for impPkg := range pkg.Imports {
				impMod, ok := findPackageModule(impPkg)
				if ok {
					if impMod != mod {
						neededModules[impMod] = struct{}{}
					}
				} else {
					if strings.HasPrefix(impPkg, "golang.org/x/") {
						// golang.org/x modules cause problems where the dependency on
						// them isn't declared correctly sometimes.

						// golang.org/x package's module path
						goXModPath := strings.Join(strings.SplitN(impPkg, "/", 4)[:3], "/")

						if goXModPath != mod.Path {
							// Assuming latest version here should work, since golang.org/x
							// packages should be backwards compatible.
							neededModules[module.New(goXModPath, "")] = struct{}{}
						}
					} else if !module.IsPkgPathStd(impPkg) {
						return fmt.Errorf("package %v imported by %v not specified in %v/go.mod", impPkg, pkgPath, mod.Path)
					}
				}
			}
		}
		require = slices.Collect(maps.Keys(neededModules))
	}

	if mod.Path != "std" {
		// Resolve unspecified versions
		for i, req := range require {
			if req.Version == "" {
				latest, cached := ms.latestVersions[req.Path]
				if !cached {
					var err error
					latest, err = repo.GoModuleGetLatestVersion(req.Path)
					if err != nil {
						return fmt.Errorf("get latest version of %v: %w", req.Path, err)
					}
					ms.latestVersions[req.Path] = latest
				}
				require[i].Version = latest
			}
		}
	}

	// Download required packages
	for _, req := range require {
		if err := ms.fetch(req, stack); err != nil {
			return err
		}
	}

	// Register that this module is downloaded
	for pkgPath, pkg := range pkgs {
		allImpsMap := map[Package]struct{}{}
		for impPkg := range pkg.Imports {
			impMod, ok := findPackageModule(impPkg)
			if ok {
				allImpsMap[Package{impPkg, impMod.Version}] = struct{}{}
			} else {
				allImpsMap[Package{impPkg, ""}] = struct{}{}
			}
		}
		p := Package{pkgPath, mod.Version}
		ms.PackageDeps[p] = slices.Collect(maps.Keys(allImpsMap))
		ms.PackageNames[p] = pkg.Name
	}
	ms.Modules[mod] = Module{
		GoVersion:    goVer,
		Dependencies: goModRequire,
		Packages:     slices.Collect(maps.Keys(pkgs)),
	}

	return nil
}

func (ms *ModuleSet) Fetch(mod module.Module) error {
	return ms.fetch(mod, &[]module.Module{})
}

// Call this after all calls to Fetch() to fetch the minimal required
// Go version.
// Also sets GoVersion.
func (ms *ModuleSet) FetchGo() error {
	goVersion := ms.RequiredGoVersion()
	/*rep := repo.GoSource(goVersion)
	have, err := rep.Have(ms.srcDir)
	if err != nil {
		return fmt.Errorf("check go%v: %w", goVersion, err)
	} else if have { // Already fetched
		return nil
	}
	ms.OnDownload(module.New("go", goVersion), StatusDownloadStarted)
	if err := rep.Get(ms.srcDir); err != nil {
		return fmt.Errorf("get go%v: %w", goVersion, err)
	}
	ms.OnDownload(module.New("go", goVersion), StatusDownloadFinished)*/
	ms.GoVersion = goVersion
	if err := ms.Fetch(module.New("std", goVersion)); err != nil {
		return err
	}
	return nil
}

// List of all required modules and versions.
// TODO: Also consider modules not specified in go.mod,
// but imported nonetheless (we have a few of those).
func (ms *ModuleSet) ModuleBuildList(baseModules ...module.Module) []module.Module {
	// Naive implementation of Go MVS (https://research.swtch.com/vgo-mvs)

	modVersions := map[string]string{} // module path to selected version
	visited := map[module.Module]struct{}{}
	var traverse func(m module.Module)
	traverse = func(m module.Module) {
		if _, ok := visited[m]; ok {
			// Break recursion loop
			return
		}
		visited[m] = struct{}{}

		// If m is newer than this module's previous version,
		// use m's version.
		if vOld, exists := modVersions[m.Path]; !exists || semver.Compare(vOld, m.Version) < 0 {
			modVersions[m.Path] = m.Version
		}

		for _, dep := range ms.Modules[m].Dependencies {
			traverse(dep)
		}
	}
	for _, m := range baseModules {
		traverse(m)
	}

	res := make([]module.Module, 0, len(modVersions))
	for modPath, modVersion := range modVersions {
		res = append(res, module.New(modPath, modVersion))
	}
	slices.SortFunc(res, func(a, b module.Module) int {
		return strings.Compare(a.String(), b.String())
	})
	return res
}

// List of all required packages and versions.
// Maps package paths to packages.
func (ms *ModuleSet) PackageBuildList(baseModules ...module.Module) map[string]Package {
	// Naive implementation of Go MVS (https://research.swtch.com/vgo-mvs), but
	// for packages.

	pkgVersions := map[string]string{} // package path to selected version
	visited := map[Package]struct{}{}
	var traverse func(p Package)
	traverse = func(p Package) {
		if module.IsPkgPathStd(p.Path) {
			p.Version = ms.GoVersion
		}

		if _, ok := visited[p]; ok {
			return
		}
		visited[p] = struct{}{}

		if vOld, exists := pkgVersions[p.Path]; !exists || semver.Compare(vOld, p.Version) < 0 {
			pkgVersions[p.Path] = p.Version
		}

		for _, dep := range ms.PackageDeps[p] {
			traverse(dep)
		}
	}

	for _, m := range baseModules {
		for _, p := range ms.Modules[m].Packages {
			traverse(Package{p, m.Version})
		}
	}

	res := make(map[string]Package, len(pkgVersions))
	for path, ver := range pkgVersions {
		res[path] = Package{path, ver}
	}
	return res
}

// Minimum required Go version for all currently fetched packages (e.g. 1.20.1).
func (ms *ModuleSet) RequiredGoVersion() string {
	highestVersion := "1"
	for _, m := range ms.Modules {
		if go_version.Compare("go"+m.GoVersion, "go"+highestVersion) > 0 {
			highestVersion = m.GoVersion
		}
	}
	return highestVersion
}
