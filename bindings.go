package main

import (
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/refaktor/ryegen/v2/config"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/converter/typeset"
)

const builtinsCommonCode = `import (
	_env "github.com/refaktor/rye/env"
	_evaldo "github.com/refaktor/rye/evaldo"
	_runner "github.com/refaktor/rye/runner"
)

func mustBuiltin(x _env.VarBuiltin, err error) *_env.VarBuiltin {
	if err != nil {
		panic(err)
	}
	return &x
}

func builtinsContext(ps *_env.ProgramState, builtins map[string]*_env.VarBuiltin, name string) *_env.RyeCtx {
	ctx := ps.Ctx
	ps.Ctx = _env.NewEnv(ps.Ctx)
	_evaldo.RegisterVarBuiltins2(builtins, ps, name)
	newctx := ps.Ctx
	ps.Ctx = ctx
	wordIdx := ps.Idx.IndexWord(name)
	ps.Ctx.Mod(wordIdx, *newctx)
	return newctx
}

var packages = map[string]*_env.RyeCtx{}

func main() {
	_runner.DoMain(func(ps *_env.ProgramState) error {
		for pkg, builtins := range builtins {
			packages[pkg] = builtinsContext(ps, builtins, "gopkg(" + pkg + ")")
		}
		_evaldo.RegisterVarBuiltins2(map[string]*_env.VarBuiltin{
			"nil": {
				Argsn: 0,
				Fn: func(ps *_env.ProgramState, _ ..._env.Object) _env.Object {
					return *_env.NewVoid()
				},
			},
			"is-nil": {
				Argsn: 1,
				Fn: func(ps *_env.ProgramState, objs ..._env.Object) _env.Object {
					_, ok := objs[0].(_env.Void)
					return *_env.NewBoolean(ok)
				},
			},
			"import\\go": {
				Argsn: 1,
				Fn: func(ps *_env.ProgramState, args ..._env.Object) _env.Object {
					arg0, ok := args[0].(_env.String)
					if !ok {
						ps.FailureFlag = true
						return _env.NewError("expected package name string, but got " + objectType(ps, args[0]))
					}
					pkg, ok := packages[arg0.Value]
					if !ok {
						ps.FailureFlag = true
						return _env.NewError("unknown Go package \"" + arg0.Value + "\"")
					}
					return *pkg
				},
			},
		}, ps, "base")
		return nil
	})
}

`

type bindingType int

const (
	// Function/method
	bindingFunc bindingType = iota
	// E.g. .FieldName?
	bindingGetter
	// E.g. .FieldName!
	bindingSetter
	// E.g. StructName
	bindingConstructor
)

func (sym bindingType) String() string {
	switch sym {
	case bindingFunc:
		return "func"
	case bindingGetter:
		return "getter"
	case bindingSetter:
		return "setter"
	case bindingConstructor:
		return "constructor"
	default:
		panic("invalid symbol")
	}
}

func bindingTypeFromString(s string) (bindingType, bool) {
	if strings.EqualFold(s, "func") {
		return bindingFunc, true
	} else if strings.EqualFold(s, "getter") {
		return bindingGetter, true
	} else if strings.EqualFold(s, "setter") {
		return bindingSetter, true
	} else if strings.EqualFold(s, "constructor") {
		return bindingConstructor, true
	} else {
		return -1, false
	}
}

type bindingProperties struct {
	pkgPath string // package path in Rye
	recv    string // receiver type in Rye
	name    string // binding name in Rye
	exclude bool   // true -> don't generate
}

type binding struct {
	// Go code resulting in the func to be converted
	funcCode string

	// Binding type
	typ bindingType
	// Go receiver type for textual filtering (without ptrs and struct without aliases)
	recv string
	// Package of the func/type
	pkg *types.Package
	// A converter to Rye for this signature type is required
	// for the binding
	requiredConverter *types.Signature
	// Imports required by the binding code (order and element uniqueness not guaranteed)
	funcCodeImports []*types.Package

	// Binding properties. Data in here is what's mutated by binding rules.
	props bindingProperties
}

// fillProps sets the props field given a
// default binding name, receiver type and
// type qualifier.
func (b *binding) fillPropsAndRecv(bName string, tset *typeset.TypeSet) {
	b.props = bindingProperties{
		name: bName,
	}
	if b.pkg != nil {
		b.props.pkgPath = b.pkg.Path()
	}
	if b.requiredConverter.Recv() != nil {
		b.props.recv =
			converter.ReceiverRyeTypeName(b.requiredConverter.Recv().Type(), tset)
		b.recv = recvTypeNameForTextualFiltering(b.requiredConverter.Recv().Type())
	}
}
func (bf *binding) key() string {
	var b strings.Builder
	if bf.props.recv != "" {
		fmt.Fprintf(&b, "%v//", bf.props.recv)
	}
	fmt.Fprintf(&b, "%v", bf.props.name)
	return b.String()
}

func (bf *binding) binding(convName string) string {
	return fmt.Sprintf("mustBuiltin(%v(nil, nil, %v))", convName, bf.funcCode)
}

// Subset of bindingProperties
type bindingSymbol struct {
	pkgPath string
	recv    string
	name    string
}

// bindingSet represents a set of
// binding symbols. Used to check
// for conflicting bindings.
type bindingSet struct {
	// Bindings (with current properties)
	bindings []binding
	// Current symbol -> index into bindings/initialProps
	currentIdx map[bindingSymbol]int
	// Initial properties (before rules)
	initialProps []bindingProperties

	invalid bool
}

func newBindingSet() *bindingSet {
	return &bindingSet{
		currentIdx: map[bindingSymbol]int{},
	}
}

// addWithRules adds a copy of the binding funcs to the bindingSet, applying
// the renaming/exclusion rules in the config.
// If any call to this function fails, the bindingSet is invalidated.
// addedBindings is only valid until the next call of this function.
func (bs *bindingSet) addWithRules(c *config.Config, bfs []binding) (addedBindings []binding, err error) {
	defer func() {
		if err != nil {
			bs.invalid = true
		}
	}()

	if bs.invalid {
		return nil, errors.New("addWithRules called on invalid bindingSet")
	}

	if len(bs.bindings) != len(bs.initialProps) {
		panic("programmer error: bindingSet: bindings and initialProps must always have the same length")
	}
	bs.bindings = slices.Grow(bs.bindings, len(bfs))
	bs.initialProps = slices.Grow(bs.initialProps, len(bfs))
	startIdx := len(bs.bindings)
	{
		i := startIdx
		for _, bf := range bfs {
			sym := bindingSymbol{bf.props.pkgPath, bf.recv, bf.props.name}
			bs.currentIdx[sym] = i
			bs.bindings = append(bs.bindings, bf)
			bs.initialProps = append(bs.initialProps, bf.props)
			i++
		}
	}

	type renameUsage int
	const (
		renameRename renameUsage = iota
		renameCasing
		renamePkg
	)

	// Backrefs represents the '\1', '\2' etc.,
	// which are created by making a capture
	// group in the package and/or name selector.
	// We split a single slice of []byte into two
	// sections for the package and name part to
	// reduce memory allocations.
	var backrefs [][]byte

	for _, rule := range c.Rules {
		for _, bf := range bs.bindings[startIdx:] {
			backrefs = backrefs[:0]
			sym := bindingSymbol{bf.props.pkgPath, bf.recv, bf.props.name}
			bfIdx := bs.currentIdx[sym]

			if rule.Select.Package != nil {
				m := rule.Select.Package.FindSubmatch([]byte(bf.props.pkgPath))
				if len(m) == 0 || len(m[0]) != len(bf.props.pkgPath) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}
			if rule.Select.Type != "" {
				if _, ok := bindingTypeFromString(rule.Select.Type); !ok {
					return nil, c.MakeError(rule.Select.TypePos, "select: unknown symbol type: %v (expected func, getter, setter or constructor)", rule.Select.Type)
				}
				if !strings.EqualFold(rule.Select.Type, bf.typ.String()) {
					continue
				}
			}
			if rule.Select.Recv != nil {
				m := rule.Select.Recv.FindSubmatch([]byte(bf.recv))
				if len(m) == 0 || len(m[0]) != len(bf.recv) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}
			if rule.Select.Name != nil {
				m := rule.Select.Name.FindSubmatch([]byte(bf.props.name))
				if len(m) == 0 || len(m[0]) != len(bf.props.name) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}

			doRename := func(newName, newPkgPath string, usage renameUsage) error {
				if usage == renamePkg {
					if newPkgPath == "" {
						return fmt.Errorf("setting package would cause package path of (%v).%v to become empty, which is not allowed",
							bf.props.pkgPath, bf.props.name)
					}
					newName = bf.props.name // keep name
				} else {
					if newName == "" {
						return fmt.Errorf("rename would cause name of (%v).%v to become empty, which is not allowed",
							bf.props.pkgPath, bf.props.name)
					}
					newPkgPath = bf.props.pkgPath // keep pkg
				}
				if newName == bf.props.name && newPkgPath == bf.props.pkgPath {
					return nil
				}

				newSym := bindingSymbol{newPkgPath, bf.recv, newName}
				if conflictIdx, exists := bs.currentIdx[newSym]; exists && !bs.bindings[conflictIdx].props.exclude {
					conflict := bs.bindings[conflictIdx]
					var fullNewName string
					if newPkgPath != bf.props.pkgPath {
						fullNewName = "(" + newPkgPath + ")."
					}
					fullNewName += newName
					var targetName string
					if usage == renamePkg {
						targetName = newPkgPath
					} else {
						targetName = fullNewName
					}
					var originallyText string
					if init := bs.initialProps[conflictIdx]; conflict.props.name != init.name ||
						conflict.props.pkgPath != init.pkgPath {
						originallyText = fmt.Sprintf(" (originally (%v).%v)", init.pkgPath, init.name)
					}
					var errPfx string
					switch usage {
					case renameRename:
						errPfx = "renaming"
					case renameCasing:
						errPfx = "to-casing: renaming"
					case renamePkg:
						errPfx = "setting package of"
					}
					return fmt.Errorf("%v %v (%v).%v to %v would cause naming conflict with %v %v%v",
						errPfx, bf.typ, bf.props.pkgPath, bf.props.name, targetName, conflict.typ, fullNewName, originallyText)
				}

				newBf := bf
				newBf.props.pkgPath = newSym.pkgPath
				newBf.props.name = newSym.name
				bf = newBf
				bs.bindings[bfIdx] = newBf
				bs.currentIdx[newSym] = bfIdx
				delete(bs.currentIdx, sym)

				return nil
			}

			substBackrefs := func(s string) string {
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
				return strings.NewReplacer(oldnew[:]...).
					Replace(s)
			}

			if rule.Actions.Include != nil {
				newBf := bf
				newBf.props.exclude = !*rule.Actions.Include
				bs.bindings[bfIdx] = newBf
			}

			if !bf.props.exclude {
				if rule.Actions.Rename != "" {
					newName := substBackrefs(rule.Actions.Rename)
					if err := doRename(newName, "", renameRename); err != nil {
						return nil, c.MakeError(rule.Actions.RenamePos, "%v", err)
					}
				}

				if rule.Actions.ToCasing != "" {
					var newName string
					switch rule.Actions.ToCasing {
					case "kebab":
						newName = strcase.ToKebab(bf.props.name)
					case "camel":
						newName = strcase.ToCamel(bf.props.name)
					case "snake":
						newName = strcase.ToSnake(bf.props.name)
					default:
						return nil, c.MakeError(rule.Actions.ToCasingPos, "action: unknown casing: %v (expected kebab, camel or snake)", rule.Actions.ToCasing)
					}
					if err := doRename(newName, "", renameCasing); err != nil {
						return nil, c.MakeError(rule.Actions.ToCasingPos, "%v", err)
					}
				}

				if rule.Actions.SetPackage != "" {
					newPkgPath := substBackrefs(rule.Actions.SetPackage)
					if err := doRename("", newPkgPath, renamePkg); err != nil {
						return nil, c.MakeError(rule.Actions.SetPackagePos, "%v", err)
					}
				}
			}
		}
	}

	return bs.bindings[startIdx:], nil
}
