package repo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/refaktor/ryegen/v2/module"
)

const goProxyURL = "https://proxy.golang.org/"
const goSourceZipURL = "https://github.com/golang/go/archive/refs/tags/"

func GoModuleGetLatestVersion(modPath string) (string, error) {
	var err error
	modPath, err = module.EscapePath(modPath)
	if err != nil {
		return "", err
	}

	url := goProxyURL + modPath + "/@latest"
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("get %v: %v: %v", url, resp.Status, string(b))
		}
		return "", fmt.Errorf("get %v: %v", url, resp.Status)
	}

	var data struct {
		Version string
		Time    string
		Origin  *struct {
			VCS  string
			URL  string
			Ref  string
			Hash string
		}
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return "", err
	}

	return data.Version, nil
}

// Repo of a Go module.
func GoModule(modPath, version string) (repo *Repo, err error) {
	modEsc, err := module.New(modPath, version).Escape()
	if err != nil {
		return nil, err
	}
	if modEsc.Version == "" {
		return nil, fmt.Errorf("expected module %v to have version", modEsc.Path)
	}
	modUnesc, err := modEsc.Unscape()
	if err != nil {
		return nil, err
	}

	return &Repo{
		DestSubdir:     modEsc.String(),
		ZipURL:         goProxyURL + modEsc.Path + "/@v/" + modEsc.Version + ".zip",
		ContentsSubdir: modUnesc.String(),
	}, nil
}

// e.g. 1.21 => 1.21.0
func makeGoStdlibVersionValid(version string) string {
	if strings.Count(version, ".") == 1 {
		sp := strings.Split(version, ".")
		min, err := strconv.Atoi(sp[0])
		if err != nil {
			return version
		}
		maj, err := strconv.Atoi(sp[1])
		if err != nil {
			return version
		}
		if min == 1 && maj >= 21 {
			return version + ".0"
		}
	}
	return version
}

// Repo of the source code for the Go programming language.
func GoStdlib(goVersion string) *Repo {
	goVersion = makeGoStdlibVersionValid(goVersion)
	return &Repo{
		DestSubdir:     "go@" + goVersion,
		ZipURL:         goSourceZipURL + "go" + goVersion + ".zip",
		ContentsSubdir: "go-go" + goVersion + "/src",
	}
}
