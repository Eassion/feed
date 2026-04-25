package upload

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Store struct {
	rootDir    string
	publicPath string
}

func NewStore(rootDir, publicPath string) *Store {
	return &Store{
		rootDir:    rootDir,
		publicPath: strings.TrimRight(publicPath, "/"),
	}
}

func (s *Store) Save(objectKey string, content io.Reader) error {
	if s == nil {
		return fmt.Errorf("upload store is not configured")
	}

	fullPath, err := s.resolvePath(objectKey)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), "upload-*")
	if err != nil {
		return err
	}

	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmpFile, content); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, fullPath)
}

func (s *Store) Open(objectKey string) (*os.File, error) {
	if s == nil {
		return nil, fmt.Errorf("upload store is not configured")
	}

	fullPath, err := s.resolvePath(objectKey)
	if err != nil {
		return nil, err
	}

	return os.Open(fullPath)
}

func (s *Store) Path(objectKey string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("upload store is not configured")
	}

	return s.resolvePath(objectKey)
}

func (s *Store) URL(objectKey string) string {
	if s == nil {
		return ""
	}

	return fmt.Sprintf("%s/%s", s.publicPath, strings.TrimLeft(objectKey, "/"))
}

func (s *Store) ObjectKeyFromURL(rawURL string) (string, bool) {
	if s == nil {
		return "", false
	}

	value := strings.TrimSpace(rawURL)
	if value == "" {
		return "", false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}

	prefix := s.publicPath + "/"
	if strings.HasPrefix(value, prefix) {
		objectKey := strings.TrimLeft(strings.TrimPrefix(value, prefix), "/")
		if _, err := s.resolvePath(objectKey); err == nil {
			return objectKey, true
		}
		return "", false
	}

	if !strings.HasPrefix(value, "/") {
		objectKey := path.Clean(value)
		if _, err := s.resolvePath(objectKey); err == nil {
			return objectKey, true
		}
	}

	return "", false
}

func (s *Store) resolvePath(objectKey string) (string, error) {
	cleanKey := path.Clean(strings.TrimSpace(objectKey))
	if cleanKey == "." || cleanKey == "" || strings.HasPrefix(cleanKey, "../") || cleanKey == ".." || path.IsAbs(cleanKey) {
		return "", fmt.Errorf("invalid object key")
	}

	fullPath := filepath.Join(s.rootDir, filepath.FromSlash(cleanKey))
	rootClean := filepath.Clean(s.rootDir)
	fullClean := filepath.Clean(fullPath)
	if fullClean != rootClean && !strings.HasPrefix(fullClean, rootClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid object key")
	}

	return fullClean, nil
}
