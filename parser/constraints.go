package parser

import (
	"go/ast"
	"go/build/constraint"
	"slices"
	"strings"
)

var (
	GOOSsuffixes   = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos"}
	GOARCHsuffixes = []string{"386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe", "loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le", "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"}
	UnixOSes       = []string{"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "linux", "netbsd", "openbsd", "solaris"}
)

// fullConstraints returns a constraint expression for all
// go:/+ build constraints and all filename build constraints.
// Returns nil if there are no constraints.
func fullConstraints(f *ast.File, filename string) (constraint.Expr, error) {
	var resExpr constraint.Expr
	add := func(expr constraint.Expr) {
		if resExpr == nil {
			resExpr = expr
		} else {
			resExpr = &constraint.AndExpr{
				X: resExpr,
				Y: expr,
			}
		}
	}
	goos, goarch := filenameSuffixConstraints(filename)
	if goos != "" {
		add(&constraint.TagExpr{Tag: goos})
	}
	if goarch != "" {
		add(&constraint.TagExpr{Tag: goarch})
	}
	for _, c := range f.Comments {
		for _, c := range c.List {
			if !constraint.IsGoBuild(c.Text) && !constraint.IsPlusBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil {
				return nil, err
			}
			add(expr)
		}
	}
	return resExpr, nil
}

// constraintTags returns all tags referenced anywhere in expr.
func constraintTags(expr constraint.Expr) (tags []string) {
	var visit func(e constraint.Expr)
	visit = func(e constraint.Expr) {
		switch e := e.(type) {
		case *constraint.AndExpr:
			visit(e.X)
			visit(e.Y)
		case *constraint.OrExpr:
			visit(e.X)
			visit(e.Y)
		case *constraint.NotExpr:
			visit(e.X)
		case *constraint.TagExpr:
			if !slices.Contains(tags, e.Tag) {
				tags = append(tags, e.Tag)
			}
		}
	}
	visit(expr)
	return
}

func filenameSuffixConstraints(filename string) (goosConstraint, goarchConstraint string) {
	for _, goos := range GOOSsuffixes {
		if strings.HasSuffix(filename, "_"+goos+".go") {
			return goos, ""
		}
	}
	for _, goarch := range GOARCHsuffixes {
		if strings.HasSuffix(filename, "_"+goarch+".go") {
			for _, goos := range GOOSsuffixes {
				if strings.HasSuffix(filename, "_"+goos+"_"+goarch+".go") {
					return goos, goarch
				}
			}
			return "", goarch
		}
	}
	return "", ""
}
