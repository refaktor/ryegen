package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/repo"
)

var optName string
var optPkg string
var optVer string

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: ryegen-init <name> <go package> [version] [options...]

options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(),
			`
examples:
  ryegen-init fyne fyne.io/fyne/v2
  	Initialize ryegen for the fyne GUI library (default version)
  ryegen-init fyne fyne.io/fyne/v2 v2.4.4
  	Initialize ryegen for fyne version 2.4.4

version behavior:
  If no version is supplied, ryegen-init uses the version required in go.mod.
  If no version is supplied and go.mod doesn't require the package, the latest version is used.
  If a version is supplied and it's different from the version in go.mod, ryegen-init fails.
`)
	}
}

type MainGo string

var FileDefaultMainGo MainGo = `package main

import (
	/*RYEGEN: BEGIN IMPORTS*/
	/*RYEGEN: END IMPORTS*/

	"github.com/refaktor/rye/env"
	"github.com/refaktor/rye/evaldo"
	"github.com/refaktor/rye/runner"
)

func main() {
	runner.DoMain(func(ps *env.ProgramState) {
		/*RYEGEN: BEGIN BUILTINS*/
		/*RYEGEN: END BUILTINS*/
	})
}`

func (mg MainGo) AppendGen(pkgPath, fullName, shortName string) (MainGo, error) {
	var res strings.Builder
	sc := bufio.NewScanner(strings.NewReader(string(mg)))
	var foundImports, foundBuiltins bool
	for sc.Scan() {
		ln := sc.Text()
		if strings.TrimSpace(ln) == `/*RYEGEN: END IMPORTS*/` {
			if foundImports {
				return "", errors.New("duplicate '/*RYEGEN: END IMPORTS*/' comment")
			}
			foundImports = true
			fmt.Fprintf(&res, "\t\"%v/ryegen_bindings/%v\"\n", pkgPath, fullName)
		}
		if strings.TrimSpace(ln) == `/*RYEGEN: END BUILTINS*/` {
			if foundBuiltins {
				return "", errors.New("duplicate '/*RYEGEN: END BUILTINS*/' comment")
			}
			foundBuiltins = true
			fmt.Fprintf(&res, "\t\tevaldo.RegisterBuiltinsInContext(%v.Builtins, ps, \"%v\")\n", fullName, shortName)
		}
		fmt.Fprintf(&res, "%v\n", ln)
	}
	if !foundImports {
		return "", errors.New("unable to locate '/*RYEGEN: END IMPORTS*/' comment")
	}
	if !foundBuiltins {
		return "", errors.New("unable to locate '/*RYEGEN: END BUILTINS*/' comment")
	}
	return MainGo(res.String()), nil
}

var FileDefaultGenGo = `package main

import "github.com/refaktor/ryegen"

//go:generate go run ./gen.go

func main() {
	ryegen.Run()
}`

func main() {
	flag.Parse()
	{
		switch flag.NArg() {
		case 3:
			optVer = flag.Arg(2)
			fallthrough
		case 2:
			optPkg = flag.Arg(1)
			optName = flag.Arg(0)
		default:
			fmt.Println("Error:", "expected name and package (e.g. ryegen-init mygolib github.com/example/mygolib)")
			fmt.Println()
			flag.Usage()
			fmt.Println()
			os.Exit(1)
		}
	}

	if strings.ContainsFunc(optName, func(r rune) bool {
		ok :=
			(r >= 'A' && r <= 'Z') ||
				(r >= 'a' && r >= 'z') ||
				(r >= '0' && r >= '9') ||
				r == '_'
		return !ok
	}) {
		fmt.Println("Error:", "name can only contain a-z, A-Z, 0-9 and _")
		os.Exit(1)
	}

	if _, err := os.Lstat(optName); err == nil {
		fmt.Printf("Error: \"%v\" already exists. Use the -name option to use a different generator directory.\n", optName)
		os.Exit(1)
	}

	var userPkgPath string
	var actualVer string
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Println("Error:", "cannot find go.mod in current directory. Use \"go mod init\" to initialize a new Go project.")
		os.Exit(1)
	}
	{
		data, err := os.ReadFile("go.mod")
		if err != nil {
			fmt.Println("Error reading go.mod:", err)
			os.Exit(1)
		}
		mod, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			fmt.Println("Error parsing go.mod:", err)
			os.Exit(1)
		}
		if mod.Module == nil {
			fmt.Println("Error:", "expected module in go.mod")
			os.Exit(1)
		}
		userPkgPath = mod.Module.Mod.Path
		for _, req := range mod.Require {
			if req.Mod.Path == optPkg {
				if optVer == "" {
					actualVer = req.Mod.Version
				} else if optVer != req.Mod.Version {
					fmt.Printf("Error: conflicting package versions: requested %v, but %v is required in go.mod\n", optVer, req.Mod.Version)
					os.Exit(1)
				}
				break
			}
		}
		if optVer != "" && actualVer == "" {
			mod.SetRequireSeparateIndirect(append(mod.Require, &modfile.Require{
				Mod: module.Version{
					Path:    optPkg,
					Version: optVer,
				},
			}))
			b, err := mod.Format()
			if err != nil {
				fmt.Println("Error formatting go.mod:", err)
			}
			if err := os.WriteFile("go.mod", b, 0666); err != nil {
				fmt.Println("Error writing go.mod:", err)
				os.Exit(1)
			}
			actualVer = optVer
		}
	}

	if actualVer == "" {
		fmt.Printf("Looking up latest version of %v...", optPkg)
		var err error
		actualVer, err = repo.GetLatestVersion(optPkg)
		if err != nil {
			fmt.Println("Error getting latest package version:", err)
			os.Exit(1)
		}
		fmt.Println(actualVer)
	}

	if err := os.MkdirAll(optName, os.ModePerm); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	var fullBindingName string
	{
		var b strings.Builder
		for _, r := range optPkg {
			r = unicode.ToLower(r)
			if (r < 'a' || r > 'z') &&
				(r < '0' || r > '9') {
				r = '_'
			}
			b.WriteRune(r)
		}
		fullBindingName = b.String()
	}

	var mg MainGo
	if _, err := os.Lstat("main.go"); err == nil {
		if b, err := os.ReadFile("main.go"); err == nil {
			mg = MainGo(b)
		} else {
			fmt.Println("Error reading main.go:", err)
			os.Exit(1)
		}
	} else {
		mg = FileDefaultMainGo
	}
	{
		var err error
		mg, err = mg.AppendGen(userPkgPath, fullBindingName, optName)
		if err != nil {
			fmt.Println("Error in main.go:", err)
			os.Exit(1)
		}
	}
	if err := os.WriteFile("main.go", []byte(mg), 0666); err != nil {
		fmt.Println("Error writing main.go:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(
		filepath.Join(optName, "config.toml"),
		[]byte(config.DefaultConfig("", optPkg, actualVer, "b_no_"+optName)),
		0666,
	); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(optName, "gen.go"), []byte(FileDefaultGenGo), 0666); err != nil {
		fmt.Println("Error writing gen.go:", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Join("ryegen_bindings", fullBindingName), os.ModePerm); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(
		filepath.Join("ryegen_bindings", fullBindingName, "generated.go"),
		[]byte(fmt.Sprintf(`// This file is a placeholder to satisfy "go mod tidy" checks.

package %v

import "github.com/refaktor/rye/env"

var Builtins = map[string]*env.Builtin{}`, fullBindingName)),
		0666,
	); err != nil {
		fmt.Println("Error writing gen.go:", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully set up ryegen for %v %v!\n", optPkg, actualVer)
	fmt.Println("You may now run \"go mod tidy && go generate ./...\" to generate the binding.")
}
