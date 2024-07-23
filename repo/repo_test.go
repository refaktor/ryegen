package repo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/refaktor/ryegen/repo"
)

func testRepo(t *testing.T, dir, pkg, version, wantFile string) {
	t.Log("Downloading", pkg, version)

	path, err := repo.Get(dir, pkg, version)
	if err != nil {
		t.Fatal(err)
	}

	if version != "" && version != "latest" {
		gotPath := filepath.ToSlash(path)
		var wantPath string
		if pkg == "std" {
			wantPath = filepath.ToSlash(filepath.Join(dir, "go-go"+version, "src"))
		} else {
			wantPath = filepath.ToSlash(filepath.Join(dir, strings.ToLower(pkg)+"@"+version))
		}
		if gotPath != wantPath {
			t.Fatalf("expected path %v, but got %v", wantPath, gotPath)
		}
	}

	if wantFile != "" {
		wantFile := filepath.Join(path, wantFile)
		if _, err := os.Stat(wantFile); err != nil {
			t.Fatalf("expected file %v to exist in archive, but not found", wantFile)
		}
	}
}

func TestRepo(t *testing.T) {
	// Regular library
	testRepo(t, "test-out", "golang.org/x/crypto", "v0.23.0", "ssh/terminal/terminal.go")
	// Capital letters
	testRepo(t, "test-out", "github.com/BurntSushi/toml", "v1.3.2", "")
	// No go.mod
	testRepo(t, "test-out", "github.com/fogleman/gg", "", "gradient.go")
	// Latest version
	testRepo(t, "test-out", "github.com/BurntSushi/toml", "latest", "")
	// Go stdlib
	testRepo(t, "test-out", "std", "1.21.5", "bytes/buffer.go")

	// Windows tends to complain of simultaneous access (although all files were closed).
	time.Sleep(500 * time.Millisecond)

	if err := os.RemoveAll("test-out"); err != nil {
		t.Fatal(err)
	}
}
