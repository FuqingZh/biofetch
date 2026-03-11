package tomlx

import (
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml/v2"
)

func ReadFileIfExists(filePath string, value any) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read manifest: %w", err)
	}

	if err := toml.Unmarshal(data, value); err != nil {
		return false, fmt.Errorf("decode manifest: %w", err)
	}
	return true, nil
}

func WriteFileAtomic(filePath string, value any) error {
	fileTemp := filePath + ".tmp"
	fileOut, err := os.Create(fileTemp)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	encoder := toml.NewEncoder(fileOut)
	encoder.SetIndentTables(true)
	errEncode := encoder.Encode(value)
	errClose := fileOut.Close()
	if errEncode != nil {
		_ = os.Remove(fileTemp)
		return errEncode
	}
	if errClose != nil {
		_ = os.Remove(fileTemp)
		return fmt.Errorf("close manifest: %w", errClose)
	}
	if err := os.Rename(fileTemp, filePath); err != nil {
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}
