package repo_test

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/refaktor/ryegen/v2/repo"
)

func TestRepo(t *testing.T) {
	const outDir = "_test_out"

	getGoModule := func(modPath, version string) *repo.Repo {
		rep, err := repo.GoModule(modPath, version)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	expectFile := func(rep *repo.Repo, path string) {
		wantPath := filepath.Join(outDir, rep.DestSubdir, path)
		if _, err := os.Stat(wantPath); err != nil {
			t.Fatalf("expected file %v to exist in archive, but not found", wantPath)
		}
	}

	testGoModule := func(modPath, version, expectFilePath string) {
		rep := getGoModule(modPath, version)
		log.Printf("downloading %v@%v...\n", modPath, version)
		if err := rep.Get(outDir); err != nil {
			t.Fatal(err)
		}
		log.Println("done, checking...")
		if expectFilePath != "" {
			expectFile(rep, expectFilePath)
		}
		log.Println("ok")
	}

	testGoStdlib := func(goVersion, expectFilePath string) {
		rep := repo.GoStdlib(goVersion)
		log.Printf("downloading go@%v...\n", goVersion)
		if err := rep.Get(outDir); err != nil {
			t.Fatal(err)
		}
		log.Println("done, checking...")
		if expectFilePath != "" {
			expectFile(rep, expectFilePath)
		}
		log.Println("ok")
	}

	// Regular library
	testGoModule("golang.org/x/crypto", "v0.23.0", "ssh/terminal/terminal.go")
	// Module requiring escaping
	testGoModule("github.com/BurntSushi/toml", "v1.3.2", "lex.go")
	// No go.mod
	{
		version, err := repo.GoModuleGetLatestVersion("github.com/fogleman/gg")
		if err != nil {
			t.Fatal(err)
		}
		testGoModule("github.com/fogleman/gg", version, "gradient.go")
	}
	// Go source code / stdlib
	testGoStdlib("1.21.5", "bytes/buffer.go")

	if err := os.RemoveAll(outDir); err != nil {
		/*var errno syscall.Errno
		if errors.As(err, &errno) {
			t.Error(errno, uintptr(errno), errno.Error())
		}*/
		t.Fatal(err, reflect.TypeOf(err.(*fs.PathError).Unwrap()))
	}
}
