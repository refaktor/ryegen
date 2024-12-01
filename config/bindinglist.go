package config

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"
)

type BindingList struct {
	Enabled map[string]bool
	Renames map[string]string
	Export  map[string]struct{}
}

func NewBindingList() *BindingList {
	return &BindingList{
		Enabled: make(map[string]bool),
		Renames: make(map[string]string),
		Export:  make(map[string]struct{}),
	}
}

func LoadBindingListFromFile(filename string) (*BindingList, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	res := NewBindingList()

	type section int
	const (
		sectionNone section = iota
		sectionExport
		sectionEnabled
		sectionDisabled
	)

	currSection := sectionNone
	sc := bufio.NewScanner(f)
	for lineNum := 1; sc.Scan(); lineNum++ {
		makeErr := func(format string, a ...any) error {
			return fmt.Errorf("%v: line %v: %v", filename, lineNum, fmt.Errorf(format, a...))
		}

		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			switch line {
			case "[export]":
				currSection = sectionExport
			case "[enabled]":
				currSection = sectionEnabled
			case "[disabled]":
				currSection = sectionDisabled
			default:
				return nil, makeErr("invalid section name %v", line)
			}
		}
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return unicode.IsSpace(r)
		})
		if len(fields) == 0 {
			panic("expected line to be nonempty")
		}
		name := fields[0]
		if currSection == sectionNone {
			return nil, makeErr("expected binding name \"%v\" to be under a section ([enabled], [disabled], or [export])", name)
		}
		if len(fields) >= 2 && fields[1] == "=>" {
			if currSection == sectionExport {
				return nil, makeErr("rename (\"=>\") not allowed in [export] section")
			}
			if len(fields) < 3 {
				return nil, makeErr("expected new name after \"=>\" (rename)")
			}
			rename := fields[2]
			if strings.Contains(rename, "//") {
				return nil, makeErr("rename string cannot contain \"//\"; do not include the receiver in the rename string")
			}
			res.Renames[name] = rename
		}
		switch currSection {
		case sectionExport:
			res.Export[name] = struct{}{}
		case sectionEnabled:
			if v, ok := res.Enabled[name]; ok && !v {
				return nil, makeErr("cannot have binding \"%v\" in both [enabled] and [disabled] sections", name)
			}
			res.Enabled[name] = true
		case sectionDisabled:
			if v, ok := res.Enabled[name]; ok && v {
				return nil, makeErr("cannot have binding \"%v\" in both [enabled] and [disabled] sections", name)
			}
			res.Enabled[name] = false
		}
	}
	return res, nil
}

func (bl *BindingList) SaveToFile(filename string, bindingFuncsToDocstrs map[string]string) error {
	isEnabled := maps.Clone(bl.Enabled)
	for name := range bindingFuncsToDocstrs {
		if _, ok := isEnabled[name]; !ok {
			isEnabled[name] = true
		}
	}

	var enabledBindings []string
	var disabledBindings []string
	for name, enabled := range isEnabled {
		if enabled {
			enabledBindings = append(enabledBindings, name)
		} else {
			disabledBindings = append(disabledBindings, name)
		}
	}
	slices.Sort(enabledBindings)
	slices.Sort(disabledBindings)

	var res bytes.Buffer
	fmt.Fprintln(&res, "# This file contains a list of bindings, which can be enabled/disabled by placing them under the according section.")
	fmt.Fprintln(&res, "# Re-run `go generate ./...` to update and sort the list.")
	fmt.Fprintln(&res, "# Renaming a binding: e.g. `some-func => my-some-func` or `Go(*X)//method => my-method`")
	fmt.Fprintln(&res, "# Bindings placed in the export section will be exposed as a public function in the generated file.")

	fmt.Fprintln(&res)
	writeBindings := func(bs []string, allowRename bool) {
		getRenameStr := func(name string) string {
			if !allowRename {
				return ""
			}
			if s, ok := bl.Renames[name]; ok {
				return " => " + s
			}
			return ""
		}

		maxCol0Len := 0
		for _, name := range bs {
			col0 := name + getRenameStr(name)
			if len(col0) > maxCol0Len {
				maxCol0Len = len(col0)
			}
		}

		for _, name := range bs {
			if docstr, ok := bindingFuncsToDocstrs[name]; ok {
				col0 := name + getRenameStr(name)
				fmt.Fprintf(
					&res,
					"%v %v\"%v\"\n",
					col0,
					strings.Repeat(" ", maxCol0Len-len(col0)),
					docstr,
				)
			}
		}
	}
	fmt.Fprintln(&res, "[export]")
	writeBindings(slices.Collect(maps.Keys(bl.Export)), false)
	fmt.Fprintln(&res)
	fmt.Fprintln(&res, "[enabled]")
	writeBindings(enabledBindings, true)
	fmt.Fprintln(&res)
	fmt.Fprintln(&res, "[disabled]")
	writeBindings(disabledBindings, true)

	if err := os.WriteFile(filename, res.Bytes(), 0666); err != nil {
		return err
	}
	return nil
}
