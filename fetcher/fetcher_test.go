package fetcher_test

import (
	"log"
	"testing"

	"github.com/refaktor/ryegen/v2/fetcher"
	"github.com/refaktor/ryegen/v2/module"
	"github.com/stretchr/testify/require"
)

func TestFetcher(t *testing.T) {
	require := require.New(t)

	modules, err := fetcher.Fetch(
		"_ryegen",
		[]module.Module{module.NewModule("fyne.io/fyne/v2", "v2.6.0")},
		fetcher.Options{
			CacheFilePath: "_ryegen/ryegen_modcache.gob",
			OnDownloadModule: func(m module.Module) {
				log.Println("downloading", m)
			},
		},
		[]string{"windows", "amd64", "cgo"},
	)
	require.NoError(err)

	log.Println(modules)
}
