package repo

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const proxyURL = "https://proxy.golang.org/"
const goZipURL = "https://github.com/golang/go/archive/refs/tags/"

func proxyRequestURL(proxyURL, pkg string, path ...string) (string, error) {
	pkg = strings.ToLower(pkg)
	pkgElems := strings.Split(pkg, "/")

	u, err := url.Parse(proxyURL)
	if err != nil {
		return "", err
	}
	u = u.JoinPath(append(pkgElems, path...)...)

	return u.String(), nil
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

func pkgPath(pkg, version string) string {
	if pkg == "std" {
		return filepath.Join("go-go"+makeGoStdlibVersionValid(version), "src")
	} else {
		pkg = strings.ToLower(pkg)
		pkgElems := strings.Split(pkg, "/")

		var pathElems []string
		pathElems = append(pathElems, pkgElems[:len(pkgElems)-1]...)
		pathElems = append(pathElems, pkgElems[len(pkgElems)-1]+"@"+version)
		return filepath.Join(pathElems...)
	}
}

// GetLatestVersion tries to retrieve the latest version given a package path.
func GetLatestVersion(pkg string) (string, error) {
	if pkg == "std" {
		return "", errors.New("cannot get latest version for pkg std")
	}

	url, err := proxyRequestURL(proxyURL, pkg, "@latest")
	if err != nil {
		return "", err
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
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

// Have checks if a specific package is already downloaded.
//
// Params are the same as for [Get].
// Always returns a valid outPath and exactVersion if err == nil.
func Have(dstPath, pkg, version string) (have bool, outPath string, exactVersion string, err error) {
	if version == "" || version == "latest" {
		v, err := GetLatestVersion(pkg)
		if err != nil {
			return false, "", "", err
		}
		version = v
	}

	outPath = filepath.Join(dstPath, pkgPath(pkg, version))

	if _, err := os.Stat(outPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, outPath, version, nil
		} else {
			return false, "", "", err
		}
	}
	return true, outPath, version, nil
}

// Get downloads a Go package.
//
// pkg is the go package name, or "std" for the go std library.
// version is the semantic version (e.g. v1.0.0), "latest" for the latest version, or the go version (e.g. 1.21.5) if pkg == "std".
// Returns the file path of the downloaded package.
// To check if a package is already downloaded, see [Have].
func Get(dstPath, pkg, version string) (string, error) {
	have, outPath, version, err := Have(dstPath, pkg, version)
	if have {
		return outPath, nil
	}

	var zipURL string
	if pkg == "std" {
		zipURL = goZipURL + "go" + makeGoStdlibVersionValid(version) + ".zip"
	} else {
		zipURL, err = proxyRequestURL(proxyURL, pkg, "@v", version+".zip")
		if err != nil {
			return "", err
		}
	}

	resp, err := http.Get(zipURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", errors.New(string(data))
		}
		return "", fmt.Errorf("get %v: %v (%v)", zipURL, resp.Status, resp.StatusCode)
	}

	if err := unzip(dstPath, data); err != nil {
		return "", err
	}

	return outPath, nil
}

func unzip(dstPath string, data []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	extractFile := func(f *zip.File) error {
		path := filepath.Join(dstPath, f.Name)
		if !strings.HasPrefix(path, filepath.Clean(dstPath)+string(os.PathSeparator)) {
			return fmt.Errorf("zip: illegal file path: %v", path)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, os.ModePerm); err != nil {
				return err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer out.Close()

			in, err := f.Open()
			if err != nil {
				return err
			}
			defer in.Close()

			if _, err := io.Copy(out, in); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range archive.File {
		if err := extractFile(f); err != nil {
			return err
		}
	}
	return nil
}
