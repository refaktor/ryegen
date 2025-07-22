package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/refaktor/ryegen/v2/config"
)

type SymbolType int

const (
	// Function/method
	SymbolFunc SymbolType = iota
	// Getter (e.g. .FieldName?)
	SymbolGetter
	// Setter (e.g. .FieldName!)
	SymbolSetter
	// Constructor (e.g. StructName)
	SymbolConstructor
)

func (sym SymbolType) String() string {
	switch sym {
	case SymbolFunc:
		return "Func"
	case SymbolGetter:
		return "Getter"
	case SymbolSetter:
		return "Setter"
	case SymbolConstructor:
		return "Constructor"
	default:
		panic("invalid symbol")
	}
}

func SymbolTypeFromString(s string) (SymbolType, bool) {
	if strings.EqualFold(s, "func") {
		return SymbolFunc, true
	} else if strings.EqualFold(s, "getter") {
		return SymbolGetter, true
	} else if strings.EqualFold(s, "setter") {
		return SymbolSetter, true
	} else if strings.EqualFold(s, "constructor") {
		return SymbolConstructor, true
	} else {
		return -1, false
	}
}

type SymbolSpec struct {
	// Initial binding name (e.g. function name)
	Name string
	// Receiver type
	Recv string
	// Type of symbol (func, getter, setter, constructor etc.)
	Type SymbolType
}

func (s SymbolSpec) Symbol() Symbol {
	return Symbol{
		Name: s.Name,
		Recv: s.Recv,
	}
}

type Symbol struct {
	Name string
	Recv string
}

func NewSymbol(name, recv string) Symbol {
	return Symbol{Name: name, Recv: recv}
}

type PackageSpec struct {
	// Package path
	PkgPath string
	// Symbols (functions, constants etc.)
	Symbols []SymbolSpec
}

// ExecuteRules executes renaming rules
// on the given spec.
// Return value names is a map from package path to
// previous symbol name to new symbol name, while
// included is whether the symbol should be included.
func Execute(c *config.Config, spec []PackageSpec) (names map[string]map[Symbol]string, included map[string]map[Symbol]bool, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("execute rules: %w", err)
		}
	}()

	names = map[string]map[Symbol]string{}
	included = map[string]map[Symbol]bool{}
	existingRenamedNames := map[string]map[Symbol]bool{} // to avoid collisions
	for _, pkg := range spec {
		if _, ok := names[pkg.PkgPath]; ok {
			return nil, nil, fmt.Errorf("duplicate package: %v", pkg.PkgPath)
		}
		names[pkg.PkgPath] = map[Symbol]string{}
		included[pkg.PkgPath] = map[Symbol]bool{}
		existingRenamedNames[pkg.PkgPath] = map[Symbol]bool{}
		for _, sym := range pkg.Symbols {
			if _, ok := names[pkg.PkgPath][sym.Symbol()]; ok {
				return nil, nil, fmt.Errorf("duplicate %v symbol: %v.%v", sym.Type, pkg.PkgPath, sym.Name)
			}
			names[pkg.PkgPath][sym.Symbol()] = sym.Name
			included[pkg.PkgPath][sym.Symbol()] = true
			existingRenamedNames[pkg.PkgPath][sym.Symbol()] = true
		}
	}

	// Backrefs represents the '\1', '\2' etc.,
	// which are created by making a capture
	// group in the package and/or name selector.
	// We split a single slice of []byte into two
	// sections for the package and name part to
	// reduce memory allocations.
	var backrefs [][]byte

	for _, rule := range c.Rules {
		for _, pkg := range spec {
			backrefs = backrefs[:0]
			numPkgBackrefs := 0 // length of package section of backrefs
			if rule.Select.Package != nil {
				m := rule.Select.Package.FindSubmatch([]byte(pkg.PkgPath))
				if len(m) == 0 || len(m[0]) != len(pkg.PkgPath) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
				numPkgBackrefs = len(m) - 1
			}

			for _, sym := range pkg.Symbols {
				backrefs = backrefs[:numPkgBackrefs]
				if rule.Select.Type != "" {
					if _, ok := SymbolTypeFromString(rule.Select.Type); !ok {
						return nil, nil, fmt.Errorf("select: unknown symbol type: %v", rule.Select.Type)
					}
					if !strings.EqualFold(rule.Select.Type, sym.Type.String()) {
						continue
					}
				}
				if rule.Select.Recv != nil {
					m := rule.Select.Recv.FindSubmatch([]byte(sym.Recv))
					if len(m) == 0 || len(m[0]) != len(sym.Recv) {
						continue
					}
					backrefs = append(backrefs, m[1:]...)
				}
				if rule.Select.Name != nil {
					name := names[pkg.PkgPath][sym.Symbol()]
					m := rule.Select.Name.FindSubmatch([]byte(name))
					if len(m) == 0 || len(m[0]) != len(name) {
						continue
					}
					backrefs = append(backrefs, m[1:]...)
				}

				renameTo := func(newName string) error {
					oldName := names[pkg.PkgPath][sym.Symbol()]
					newSym := NewSymbol(newName, sym.Recv)
					if existingRenamedNames[pkg.PkgPath][newSym] {
						return fmt.Errorf("renaming %v to %v would cause a conflict",
							strconv.Quote(oldName), strconv.Quote(newName))
					}
					names[pkg.PkgPath][sym.Symbol()] = newName
					existingRenamedNames[pkg.PkgPath][newSym] = false
					existingRenamedNames[pkg.PkgPath][newSym] = true
					return nil
				}

				if rule.Actions.Rename != "" {
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
					newName := strings.NewReplacer(oldnew[:]...).
						Replace(rule.Actions.Rename)
					if err := renameTo(newName); err != nil {
						return nil, nil, err
					}
				}

				if rule.Actions.Include != nil {
					included[pkg.PkgPath][sym.Symbol()] = *rule.Actions.Include
				}

				if rule.Actions.ToCasing != "" {
					name := names[pkg.PkgPath][sym.Symbol()]
					var newName string
					switch rule.Actions.ToCasing {
					case "kebab":
						newName = strcase.ToKebab(name)
					case "camel":
						newName = strcase.ToCamel(name)
					case "snake":
						newName = strcase.ToSnake(name)
					default:
						return nil, nil, fmt.Errorf("action: unknown casing: %v", rule.Actions.ToCasing)
					}
					if err := renameTo(newName); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}

	return
}
