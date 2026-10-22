package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
)

//go:embed default.yaml
var defaultYAML []byte

// DefaultYAML returns the documented default configuration file.
func DefaultYAML() []byte { return defaultYAML }

// GenerateDefaultConfig writes the default configuration to filename. It
// refuses to overwrite an existing file unless force is set.
func GenerateDefaultConfig(filename string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(filename, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists (use --force to overwrite)", filename)
		}
		return err
	}
	if _, err := f.Write(defaultYAML); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
