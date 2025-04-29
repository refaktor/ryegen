package repo

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Repo struct {
	// Directory to place contents into.
	DestSubdir string
	// URL of ZIP file.
	ZipURL string
	// Sub-directory in archive to pull contents from.
	ContentsSubdir string
}

func (r *Repo) Have(destToplevelDir string) (have bool, err error) {
	destDir := filepath.Join(destToplevelDir, r.DestSubdir)
	info, err := os.Stat(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("expected %v to be a directory", destDir)
	}

	if lockInfo, err := os.Stat(filepath.Join(destDir, ".repo_incomplete_lock")); err == nil {
		if !lockInfo.Mode().IsRegular() {
			return false, fmt.Errorf("expected .repo_incomplete_lock to be a regular file")
		}
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}

	return true, nil
}

func (r *Repo) Get(destToplevelDir string) error {
	resp, err := http.Get(r.ZipURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("get %v: %v: %v", r.ZipURL, resp.Status, string(data))
		}
		return fmt.Errorf("get %v: %v", r.ZipURL, resp.Status)
	}

	destDir := filepath.Join(destToplevelDir, r.DestSubdir)

	{
		if err := os.RemoveAll(destDir); err != nil {
			return err
		}

		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			return err
		}

		if f, err := os.Create(filepath.Join(destDir, ".repo_incomplete_lock")); err == nil {
			f.Close()
		} else {
			return err
		}

		if err := unzip(destDir, r.ContentsSubdir, data); err != nil {
			return err
		}

		if err := os.Remove(filepath.Join(destDir, ".repo_incomplete_lock")); err != nil {
			return err
		}
	}

	return nil
}

func unzip(dstPath string, contentsSubdir string, data []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	extractFile := func(f *zip.File) error {
		if !strings.HasPrefix(f.Name, contentsSubdir) {
			return nil
		}
		path := filepath.Clean(filepath.Join(dstPath, strings.TrimPrefix(f.Name, contentsSubdir)))
		{
			wantPfx := filepath.Clean(dstPath) + string(filepath.Separator)
			if !strings.HasPrefix(path+string(filepath.Separator), wantPfx) {
				return fmt.Errorf("zip: illegal file path: %v (expected prefix %v)", path, wantPfx)
			}
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
