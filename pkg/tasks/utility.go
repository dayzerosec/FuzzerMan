package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// GetWorkDir is a quick utility function to just ensure a directory exists under the work path and provides the
// absolute path to the subfolder
func GetWorkDir(workpath string, folder ...string) (string, error) {
	folder = append([]string{workpath}, folder...)
	fullpath := filepath.Join(folder...)

	absWorkPath, err := filepath.Abs(workpath)
	if err != nil {
		return "", err
	}

	absFullPath, err := filepath.Abs(fullpath)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absFullPath, absWorkPath) {
		return "", errors.New(fmt.Sprintf("potential path traversal: %s", fullpath))
	}

	if stat, err := os.Stat(fullpath); err == nil {
		if !stat.IsDir() {
			return "", errors.New(fmt.Sprintf("'%s' already exists, but is not a directory", fullpath))
		}
	} else {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err = os.MkdirAll(fullpath, 0771); err != nil {
			return "", errors.New(fmt.Sprintf("failed to create %s", fullpath))
		}
	}

	return absFullPath, nil
}

// MultipartFileUpload will perform a multi-part POST request to the given url.
func MultipartFileUpload(client *http.Client, url string, values map[string]io.Reader) (err error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for key, r := range values {
		var fw io.Writer
		if x, ok := r.(*os.File); ok {
			if fw, err = w.CreateFormFile(key, filepath.Base(x.Name())); err != nil {
				return
			}
		} else {
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}

		if _, err = io.Copy(fw, r); err != nil {
			return
		}
	}
	_ = w.Close()

	req, err := http.NewRequest("POST", url, &b)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	res, err := client.Do(req)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("bad status: %s", res.Status)
	}

	return
}
