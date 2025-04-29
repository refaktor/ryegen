package bindspec

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Parse a bindspec from source.
// filename is for errors.
func Parse(filename string, src []byte) ([]*Stmt, error) {
	type word struct {
		word string
		line int
	}

	var words []word
	{
		line := 1
		for ln := range bytes.SplitSeq(src, []byte("\n")) {
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
				stmt = &Stmt{}
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
				switch words[i].word {
				case "pkg":
					if stmt.PkgSelector != nil {
						return nil, errorHere(2, "duplicate package selector")
					}
					stmt.NotPkg = negateNext
					stmt.PkgSelector = re
				case "name":
					if stmt.NameSelector != nil {
						return nil, errorHere(2, "duplicate name selector")
					}
					stmt.NotName = negateNext
					stmt.NameSelector = re
				}
				negateNext = false
				i += 2
			case "not":
				negateNext = !negateNext
				i += 1
			}
		// Action
		case "rename", "to-kebab", "exclude":
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
	return stmts, nil
}
