package backups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrChecksumMismatch = errors.New("backup checksum mismatch")

type Storage interface {
	Create(context.Context, string, string, string) (string, int64, string, error)
	Validate(context.Context, string, string) error
	Restore(context.Context, string, string) error
	Delete(context.Context, string) error
}

type LocalStorage struct {
	root string
}

func NewLocalStorage(root string) *LocalStorage {
	return &LocalStorage{root: root}
}

func (s *LocalStorage) Create(ctx context.Context, serverID, backupID, source string) (string, int64, string, error) {
	dir := filepath.Join(s.root, serverID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, "", err
	}
	path := filepath.Join(dir, backupID+".tar.gz")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, "", err
	}
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	gz := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gz)
	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeTarErr := tarWriter.Close()
	closeGzipErr := gz.Close()
	closeFileErr := file.Close()
	if walkErr != nil || closeTarErr != nil || closeGzipErr != nil || closeFileErr != nil {
		_ = os.Remove(path)
		for _, candidate := range []error{walkErr, closeTarErr, closeGzipErr, closeFileErr} {
			if candidate != nil {
				return "", 0, "", candidate
			}
		}
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "", 0, "", err
	}
	return path, stat.Size(), hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *LocalStorage) Validate(ctx context.Context, location, expectedSHA string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cleanRoot, err := filepath.Abs(s.root)
	if err != nil {
		return err
	}
	cleanLocation, err := filepath.Abs(location)
	if err != nil {
		return err
	}
	if cleanLocation != cleanRoot && !strings.HasPrefix(cleanLocation, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("backup outside storage root")
	}
	file, err := os.Open(cleanLocation)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA) {
		return ErrChecksumMismatch
	}
	return nil
}

func (s *LocalStorage) Restore(ctx context.Context, location, destination string) error {
	cleanRoot, err := filepath.Abs(s.root)
	if err != nil {
		return err
	}
	cleanLocation, err := filepath.Abs(location)
	if err != nil {
		return err
	}
	if cleanLocation != cleanRoot && !strings.HasPrefix(cleanLocation, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("backup outside storage root")
	}
	file, err := os.Open(cleanLocation)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		cleanDest, err := filepath.Abs(destination)
		if err != nil {
			return err
		}
		cleanTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("invalid backup path")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, os.FileMode(header.Mode)&0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o750); err != nil {
				return err
			}
			out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o750)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, reader, header.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func (s *LocalStorage) Delete(ctx context.Context, location string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cleanRoot, err := filepath.Abs(s.root)
	if err != nil {
		return err
	}
	cleanLocation, err := filepath.Abs(location)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(cleanLocation, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("backup outside storage root")
	}
	return os.Remove(cleanLocation)
}
