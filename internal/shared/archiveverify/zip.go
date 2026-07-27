package archiveverify

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
)

func ZIPRequiredMember(required string) func(string) error {
	return func(path string) error {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat ZIP: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("ZIP path is not a regular file: %s", path)
		}
		reader, err := zip.OpenReader(path)
		if err != nil {
			return fmt.Errorf("open ZIP: %w", err)
		}
		defer reader.Close()
		for _, file := range reader.File {
			if file.Name != required || file.FileInfo().IsDir() {
				continue
			}
			handle, err := file.Open()
			if err != nil {
				return fmt.Errorf("open required ZIP member %q: %w", required, err)
			}
			_, copyErr := io.Copy(io.Discard, handle)
			closeErr := handle.Close()
			if copyErr != nil {
				return fmt.Errorf("read required ZIP member %q: %w", required, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close required ZIP member %q: %w", required, closeErr)
			}
			return nil
		}
		return fmt.Errorf("ZIP missing required member %q", required)
	}
}
