package bindspec

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"
)

// Info defines how the bindspec interpreter
// should run commands.
type Info struct {
	// All names by package.
	PkgToNames map[string][]string
}

// Result defines the program result.
type Result struct {
	// Package and original name to new name, or old name
	// if never renamed.
	NewNames map[string]map[string]string
	// Package and original name to whether included.
	Included map[string]map[string]bool
}

// Runs the bindspec program. iface determines
// how all actions are performed.
func Run(prog *Program, info Info) (*Result, error) {
	names := map[string]map[string]string{}
	included := map[string]map[string]bool{}
	for pkg, nms := range info.PkgToNames {
		names[pkg] = map[string]string{}
		included[pkg] = map[string]bool{}
		for _, nm := range nms {
			names[pkg][nm] = nm
			included[pkg][nm] = true
		}
	}
	pkgPaths := slices.Sorted(maps.Keys(names))

	rename := func(pkg, name, newName string) {
		nameToNewName, ok := names[pkg]
		if !ok {
			panic("invalid package name")
		}
		_, ok = nameToNewName[name]
		if !ok {
			panic("invalid symbol name")
		}
		nameToNewName[name] = newName
	}

	setIncluded := func(pkg, name string, incl bool) {
		nameToIncl, ok := included[pkg]
		if !ok {
			panic("invalid package name")
		}
		_, ok = nameToIncl[name]
		if !ok {
			panic("invalid symbol name")
		}
		nameToIncl[name] = incl
	}

	type sym struct {
		pkgIdx int
		name   string
	}

	var selPkgs []string
	var selPkgBackrefs [][][]byte
	var selSyms []sym
	var selSymNameBackrefs [][][]byte
	for _, cmd := range prog.Body {
		errorHere := func(format string, args ...any) error {
			return fmt.Errorf("%v:%v: %w", prog.Filename, cmd.LineNo, fmt.Errorf(format, args...))
		}

		selPkgs = selPkgs[:0]
		selPkgBackrefs = selPkgBackrefs[:0]
		selSyms = selSyms[:0]
		selSymNameBackrefs = selSymNameBackrefs[:0]
		var selPkgSeen, selNameSeen bool
		for _, sel := range cmd.Selectors {
			switch sel.Type {
			case SelPkg:
				if selNameSeen {
					return nil, errorHere("package selector must come before name selector")
				}
				if selPkgSeen {
					return nil, errorHere("duplicate package selector")
				}
				for _, pkg := range pkgPaths {
					m := sel.Regexp.FindSubmatch([]byte(pkg))
					if m != nil {
						selPkgs = append(selPkgs, pkg)
						selPkgBackrefs = append(selPkgBackrefs, m[1:])
					}
				}
				selPkgSeen = true
			case SelName:
				if !selPkgSeen {
					for _, pkg := range pkgPaths {
						selPkgs = append(selPkgs, pkg)
						selPkgBackrefs = append(selPkgBackrefs, nil)
					}
				}
				if selNameSeen {
					return nil, errorHere("duplicate name selector")
				}
				for pkgIdx, pkg := range selPkgs {
					for name, newName := range names[pkg] {
						m := sel.Regexp.FindSubmatch([]byte(newName))
						if m != nil {
							selSyms = append(selSyms, sym{pkgIdx, name})
							selSymNameBackrefs = append(selSymNameBackrefs, m[1:])
						}
					}
				}
				selNameSeen = true
			default:
				return nil, errorHere("unknown selector type %v", sel.Type)
			}
		}
		if selPkgSeen && !selNameSeen {
			for pkgIdx, pkg := range selPkgs {
				for name := range names[pkg] {
					selSyms = append(selSyms, sym{pkgIdx, name})
					selSymNameBackrefs = append(selSymNameBackrefs, nil)
				}
			}
		}
		switch cmd.Action {
		case Rename:
			for symIdx, sym := range selSyms {
				backrefs := append(append([][]byte{},
					selPkgBackrefs[sym.pkgIdx]...),
					selSymNameBackrefs[symIdx]...)
				oldnew := [2 * 9]string{
					`\1`, "",
					`\2`, "",
					`\3`, "",
					`\4`, "",
					`\5`, "",
					`\6`, "",
					`\7`, "",
					`\8`, "",
					`\9`, "",
				}
				for i := range min(len(backrefs), 9) {
					oldnew[2*i+1] = string(backrefs[i])
				}
				rep := strings.NewReplacer(oldnew[:]...)
				newName := rep.Replace(cmd.ActionParam)
				rename(selPkgs[sym.pkgIdx], sym.name, newName)
			}
		case ToKebab:
			for _, sym := range selSyms {
				pkg := selPkgs[sym.pkgIdx]
				var newName string
				if names, ok := names[pkg]; ok {
					newName, ok = names[sym.name]
					if !ok {
						panic("invalid symbol name")
					}
				} else {
					panic("invalid package name")
				}
				rename(pkg, sym.name, strcase.ToKebab(newName))
			}
		case Include:
			for _, sym := range selSyms {
				setIncluded(selPkgs[sym.pkgIdx], sym.name, true)
			}
		case Exclude:
			for _, sym := range selSyms {
				setIncluded(selPkgs[sym.pkgIdx], sym.name, false)
			}
		default:
			return nil, errorHere("unknown action type %v", cmd.Action)
		}
	}

	return &Result{
		NewNames: names,
		Included: included,
	}, nil
}
