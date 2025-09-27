package preprocessor_test

import (
	"bytes"
	"errors"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/refaktor/ryegen/v2/preprocessor"
	"github.com/stretchr/testify/require"
)

func TestPreprocessor(t *testing.T) {
	require := require.New(t)
	ents, err := os.ReadDir("testdata")
	require.NoError(err)
	for _, ent := range ents {
		base, ok := strings.CutSuffix(ent.Name(), ".in.go")
		if !ok {
			continue
		}
		inPath := filepath.Join("testdata", ent.Name())

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, inPath, nil, parser.SkipObjectResolution|parser.ParseComments)
		require.NoError(err)
		err = preprocessor.Preprocess(fset, file, func(path string) (string, error) {
			// Assuming the last path element as import name is sufficient here.
			lastSlash := strings.LastIndex(path, "/")
			if lastSlash == -1 {
				return path, nil
			} else {
				return path[lastSlash+1:], nil
			}
		})
		require.NoError(err)
		var got []byte
		{
			var b bytes.Buffer
			require.NoError(format.Node(&b, fset, file))
			got = b.Bytes()
		}

		var want []byte
		wantPath := filepath.Join("testdata", base+".out.go")
		if _, err := os.Stat(wantPath); errors.Is(err, os.ErrNotExist) {
			require.NoError(os.WriteFile(wantPath, got, 0666))
			continue
		} else if err != nil {
			require.NoError(err)
		} else {
			var err error
			want, err = os.ReadFile(wantPath)
			require.NoError(err)
		}

		require.Equal(
			strings.TrimSpace(string(want)),
			strings.TrimSpace(string(got)),
			"actual preprocessed file doesn't match expected file",
		)
	}
}
