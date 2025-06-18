package bindspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"
)

// An interface defines how the bindspec interpreter
// should run commands.
type Interface struct {
	// All packages.
	Pkgs []string
	// All names by package.
	Names map[string][]string
	// Renames the selected symbol.
	Rename func(pkg, name, newName string)
	// Includes or excludes the selected symbol.
	SetIncluded func(pkg, name string, included bool)
}

// Runs the bindspec program. iface determines
// how all actions are performed.
func Run(prog *Program, iface Interface) error {
	nameIdx := map[string]map[string]int{} // pkg and name to index
	for pkg, names := range iface.Names {
		// We'll change symbol names as we go,
		// so create a copy.
		iface.Names[pkg] = slices.Clone(names)

		nameIdx[pkg] = map[string]int{}
		for i, name := range names {
			nameIdx[pkg][name] = i
		}
	}

	// Updates the internal record, then renames. ALWAYS use this.
	doRename := func(pkg, name, newName string) {
		idxs, ok := nameIdx[pkg]
		if !ok {
			panic("invalid package name")
		}
		idx, ok := idxs[name]
		if !ok {
			panic("invalid symbol name")
		}
		iface.Names[pkg][idx] = newName
		nameIdx[pkg][newName] = idx
		iface.Rename(pkg, name, newName)
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
					return errorHere("package selector must come before name selector")
				}
				if selPkgSeen {
					return errorHere("duplicate package selector")
				}
				for _, pkg := range iface.Pkgs {
					m := sel.Regexp.FindSubmatch([]byte(pkg))
					if m != nil {
						selPkgs = append(selPkgs, pkg)
						selPkgBackrefs = append(selPkgBackrefs, m[1:])
					}
				}
			case SelName:
				if !selPkgSeen {
					selPkgs = append(selPkgs, iface.Pkgs...)
					selPkgBackrefs = append(selPkgBackrefs, nil)
				}
				if selNameSeen {
					return errorHere("duplicate name selector")
				}
				for pkgIdx, pkg := range selPkgs {
					for _, name := range iface.Names[pkg] {
						m := sel.Regexp.FindSubmatch([]byte(name))
						if m != nil {
							selSyms = append(selSyms, sym{pkgIdx, name})
							selSymNameBackrefs = append(selSymNameBackrefs, m[1:])
						}
					}
				}
			default:
				return errorHere("unknown selector type %v", sel.Type)
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
				doRename(selPkgs[sym.pkgIdx], sym.name, newName)
			}
		case ToKebab:
			for _, sym := range selSyms {
				doRename(selPkgs[sym.pkgIdx], sym.name, strcase.ToKebab(sym.name))
			}
		case Include:
			for _, sym := range selSyms {
				iface.SetIncluded(selPkgs[sym.pkgIdx], sym.name, true)
			}
		case Exclude:
			for _, sym := range selSyms {
				iface.SetIncluded(selPkgs[sym.pkgIdx], sym.name, false)
			}
		default:
			return errorHere("unknown action type %v", cmd.Action)
		}
	}

	return nil
}
