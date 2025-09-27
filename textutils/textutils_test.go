package textutils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndentString(t *testing.T) {
	require := require.New(t)

	require.Equal(`  Hello
  World`,
		IndentString(`Hello
World`, "  ", 1),
	)

	require.Equal(`  Hello
  World
`,
		IndentString(`Hello
World
`, "  ", 1),
	)

	require.Equal(`  Hello
  World
`,
		IndentString(`Hello
World
  `, "  ", 1),
	)

	require.Equal(`  Hello

  World
`,
		IndentString(`Hello
  
World
`, "  ", 1),
	)
}
