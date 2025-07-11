package bindspec_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/refaktor/ryegen/v2/bindspec"
	"github.com/stretchr/testify/require"
)

var parserDir = filepath.Join("testdata", "parser")
var interpreterDir = filepath.Join("testdata", "interpreter")

func runTestParser(t *testing.T, name string) {
	t.Run(name, func(t *testing.T) {
		require := require.New(t)
		base := filepath.Join(parserDir, name)
		expectParseError, err := os.ReadFile(base + ".err")
		if !os.IsNotExist(err) {
			require.NoError(err)
		}
		src, err := os.ReadFile(base + ".bindspec")
		require.NoError(err)
		bs, err := bindspec.Parse(base+".bindspec", src)
		if expectParseError == nil {
			require.NoError(err)
			/*for _, x := range bs.Body {
				fmt.Println(x)
			}*/
			_ = bs
		} else {
			errStr := err.Error()
			errStr = strings.ReplaceAll(errStr, "\\", "/")
			expect := string(expectParseError)
			expect = strings.TrimRight(expect, "\r\n")
			expect = strings.ReplaceAll(expect, "\\", "/")
			require.EqualError(errors.New(errStr), expect)
		}
	})
}

func TestParser(t *testing.T) {
	require := require.New(t)
	dir, err := os.ReadDir(parserDir)
	require.NoError(err)
	for _, f := range dir {
		if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".bindspec") {
			runTestParser(t, strings.TrimSuffix(f.Name(), ".bindspec"))
		}
	}
}

func runTestInterpreter(t *testing.T, name string) {
	type expectStmt struct {
		lineNo   int
		lineCode string
		pkg      string
		name     string
		newName  string
	}

	t.Run(name, func(t *testing.T) {
		require := require.New(t)
		base := filepath.Join(interpreterDir, name)
		src, err := os.ReadFile(base + ".bindspec")
		require.NoError(err)

		// Parse .expect file.
		expectFilename := base + ".expect"
		var expect []expectStmt
		{
			file, err := os.ReadFile(expectFilename)
			require.NoError(err)
			file = bytes.ReplaceAll(file, []byte("\r\n"), []byte("\n"))
			lineNo := 0
			for line := range bytes.SplitSeq(file, []byte("\n")) {
				lineNo++
				fields := bytes.Fields(line)
				if len(fields) == 0 || bytes.HasPrefix(line, []byte("#")) {
					continue
				}
				lineFormatErrMsg := func() string {
					return fmt.Sprintf("Programmer error: %v:%v: line in .expect file should be of format <pkg> <name> -> <new-name>|\"@excluded\".", expectFilename, lineNo)
				}
				require.Len(fields, 4, lineFormatErrMsg())
				require.Equal(string(fields[2]), "->", lineFormatErrMsg())
				expect = append(expect, expectStmt{
					lineNo:   lineNo,
					lineCode: string(bytes.Join(fields, []byte(" "))),
					pkg:      string(fields[0]),
					name:     string(fields[1]),
					newName:  string(fields[3]),
				})
			}
		}

		interpInfo := bindspec.Info{
			PkgToNames: map[string][]string{},
		}
		for _, exp := range expect {
			interpInfo.PkgToNames[exp.pkg] = append(interpInfo.PkgToNames[exp.pkg], exp.name)
		}

		prog, err := bindspec.Parse(base+".bindspec", src)
		require.NoError(err)
		bsRes, err := bindspec.Run(prog, interpInfo)
		require.NoError(err)

		for _, exp := range expect {
			errMsg := func(msg string) string {
				return fmt.Sprintf("%v:%v (%v): %v", expectFilename, exp.lineNo, exp.lineCode, msg)
			}

			nameToIncluded := bsRes.Included[exp.pkg]
			nameToNewName := bsRes.NewNames[exp.pkg]
			require.NotNil(nameToIncluded, errMsg("expected pkg to exist in interpreter"))
			require.NotNil(nameToNewName, errMsg("expected pkg to exist in interpreter"))
			if exp.newName != "@excluded" {
				require.True(nameToIncluded[exp.name], errMsg("binding is excluded, although it is expected to exist in .expect file"))
				require.Equal(exp.newName, nameToNewName[exp.name], errMsg("got a different new name than specified in .expect file"))
			} else {
				require.False(nameToIncluded[exp.name], errMsg("binding is not excluded, although it is expected to be excluded in .expect file"))
			}
		}
	})
}

func TestInterpreter(t *testing.T) {
	require := require.New(t)
	dir, err := os.ReadDir(interpreterDir)
	require.NoError(err)
	for _, f := range dir {
		if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".bindspec") {
			runTestInterpreter(t, strings.TrimSuffix(f.Name(), ".bindspec"))
		}
	}
}
