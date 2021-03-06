package filestore

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type fileStore struct {
	base string
}

// NewLocalFileStore creates a filestore mounted on a given local directory path
func NewLocalFileStore(basedirectory Path) (FileStore, error) {
	base := filepath.Clean(string(basedirectory))
	info, err := os.Stat(string(base))
	if err != nil {
		return nil, fmt.Errorf("error getting %s info: %s", base, err.Error())
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", base)
	}
	return &fileStore{string(base)}, nil
}

func (fs fileStore) filename(p Path) string {
	return filepath.Join(fs.base, string(p))
}

func (fs fileStore) Open(p Path) (File, error) {
	name := fs.filename(p)
	if _, err := os.Stat(name); err != nil {
		return nil, fmt.Errorf("error trying to open %s: %s", name, err.Error())
	}
	return newFile(Path(fs.base), p)
}

func (fs fileStore) Create(p Path) (File, error) {
	name := fs.filename(p)
	if _, err := os.Stat(name); err == nil {
		return nil, fmt.Errorf("file %s already exists", name)
	}
	return newFile(Path(fs.base), p)
}

func (fs fileStore) Store(p Path, src File) (Path, error) {
	dest, err := fs.Create(p)
	if err != nil {
		return Path(""), err
	}

	if _, err = io.Copy(dest, src); err != nil {
		dest.Close()
		return Path(""), err
	}
	return Path(fs.filename(p)), dest.Close()
}

func (fs fileStore) Delete(p Path) error {
	filename := string(p)
	if strings.HasPrefix(filename, fs.base) {
		return os.Remove(filename)
	}
	return fmt.Errorf("invalid base path for '%s' (expecting '%s')", string(p), fs.base)
}

func (fs fileStore) CreateTemp() (File, error) {
	f, err := ioutil.TempFile(fs.base, "fstmp")
	if err != nil {
		return nil, err
	}
	filename := filepath.Base(f.Name())
	return &fd{File: f, basepath: fs.base, filename: filename}, nil
}
