package bindspec

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Parse a bindspec from source.
// filename is for errors.
func Parse(filename string, src []byte) (*Program, error) {
	type word struct {
		word string
		line int
	}

	var words []word
	{
		line := 1
		for ln := range bytes.SplitSeq(
			bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")),
			[]byte("\n"),
		) {
			for f := range strings.FieldsSeq(string(ln)) {
				if strings.HasPrefix(f, "#") {
					break
				}
				words = append(words, word{
					word: f,
					line: line,
				})
			}
			line++
		}
	}

	i := 0 // current word index
	errorHere := func(numWords int, format string, args ...any) error {
		var line int
		var contextStr string
		if i < len(words) {
			line = words[i].line
			for j := 0; ; j++ {
				if j >= numWords {
					contextStr = "at " + strconv.Quote(contextStr)
					break
				}
				if i+j >= len(words) {
					contextStr = "after " + strconv.Quote(contextStr)
					break
				}
				if j != 0 {
					contextStr += " "
				}
				contextStr += words[i+j].word
			}
		} else if len(words) > 0 {
			line = words[len(words)-1].line + 1
			contextStr = "at end of file"
		}
		return fmt.Errorf("%v:%v: %v: %w", filename, line, contextStr, fmt.Errorf(format, args...))
	}
	var stmts []*Stmt
	var stmt *Stmt // non-nil => we are within an incomplete Stmt
	negateNext := false
	for i < len(words) {
		switch words[i].word {
		// Selector
		case "name", "pkg", "not":
			if stmt == nil {
				stmt = &Stmt{
					LineNo: words[i].line,
				}
			}
			switch words[i].word {
			case "pkg", "name":
				if i+1 >= len(words) {
					return nil, errorHere(2, "expected regex word")
				}
				re, err := regexp.Compile("^" + words[i+1].word + "$")
				if err != nil {
					return nil, errorHere(2, "%w", err)
				}
				var selType SelectorType
				var selTypeStr string
				switch words[i].word {
				case "pkg":
					selType = SelPkg
					selTypeStr = "package"
					if slices.ContainsFunc(stmt.Selectors, func(sel Selector) bool {
						return sel.Type == SelName
					}) {
						return nil, errorHere(2, "package selector must come before name selector")
					}
				case "name":
					selType = SelName
					selTypeStr = "name"
				}
				if slices.ContainsFunc(stmt.Selectors, func(sel Selector) bool {
					return sel.Type == selType
				}) {
					return nil, errorHere(2, "duplicate %v selector", selTypeStr)
				}
				stmt.Selectors = append(stmt.Selectors, Selector{
					Type:   selType,
					Not:    negateNext,
					Regexp: re,
				})
				negateNext = false
				i += 2
			case "not":
				negateNext = !negateNext
				i += 1
			}
		// Action
		case "rename", "to-kebab", "include", "exclude":
			if stmt == nil {
				return nil, errorHere(1, "expected at least one selector before action")
			}
			switch words[i].word {
			case "rename":
				stmt.Action = Rename
				if i+1 >= len(words) {
					return nil, errorHere(2, "expected parameter")
				}
				stmt.ActionParam = words[i+1].word
				i += 2
			case "to-kebab":
				stmt.Action = ToKebab
				i += 1
			case "include":
				stmt.Action = Include
				i += 1
			case "exclude":
				stmt.Action = Exclude
				i += 1
			}
			stmts = append(stmts, stmt)
			stmt = nil
			negateNext = false
		default:
			return nil, errorHere(1, "expected selector or action")
		}
	}
	if stmt != nil { // incomplete stmt
		return nil, errorHere(1, "expected action after selector")
	}
	return &Program{
		Filename: filename,
		Body:     stmts,
	}, nil
}
