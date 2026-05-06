package service

import (
	"fmt"
	"log/slog"
	"os"
)

func chmodLocalConfigTempFile(file *os.File, mode os.FileMode, description string) error {
	if err := file.Chmod(mode); err != nil {
		if os.IsPermission(err) {
			slog.Warn(
				"skipping local config temp file chmod because filesystem does not support it",
				"path", file.Name(),
				"description", description,
				"error", err,
			)
			return nil
		}
		return fmt.Errorf("chmod %s temp file: %w", description, err)
	}
	return nil
}
