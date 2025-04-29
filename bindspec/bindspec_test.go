package bindspec_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/refaktor/ryegen/v2/bindspec"
	"github.com/stretchr/testify/require"
)

func TestBindspec(t *testing.T) {
	require := require.New(t)
	dir, err := os.ReadDir("testdata")
	require.NoError(err)
	for _, f := range dir {
		if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".bindspec") {
			path := filepath.Join("testdata", f.Name())
			expectParseError, err := os.ReadFile(path + ".err")
			if !os.IsNotExist(err) {
				require.NoError(err)
			}
			src, err := os.ReadFile(path)
			require.NoError(err)
			bs, err := bindspec.Parse(path, src)
			if expectParseError == nil {
				require.NoError(err)
			} else {
				expect := string(expectParseError)
				expect = strings.TrimRight(expect, "\r\n")
				if os.PathSeparator == '\\' {
					expect = strings.ReplaceAll(expect, "testdata/", "testdata\\")
				}
				require.EqualError(err, expect)
			}
			for _, x := range bs {
				fmt.Println(x)
			}
			_ = bs
		}
	}
}
