package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

type DirectoryName string

const (
	CorpusDirectory   DirectoryName = "corpus"
	TempDirectory                   = "temp"
	LogDirectory                    = "logs"
	ArtifactDirectory               = "artifacts"
)

type FileName int

const (
	MergeLockFile FileName = iota
	CloudFuzzerFile
	LocalFuzzerFile
)

func (c *Config) WorkPath(name DirectoryName) string {
	fullpath := filepath.Join(c.WorkDirectory, string(name))
	if _, err := os.Stat(fullpath); os.IsNotExist(err) {
		_ = os.MkdirAll(fullpath, 0770)
	}
	return fullpath
}

func (c *Config) CloudPath(name DirectoryName) string {
	return path.Join(c.CloudStorage.Prefix, string(name))
}

func (c *Config) FilePath(name FileName) string {
	switch name {
	case MergeLockFile:
		return path.Join(c.CloudStorage.Prefix, ".merge")
	case CloudFuzzerFile:
		return path.Join(c.CloudStorage.Prefix, "fuzzer")
	case LocalFuzzerFile:
		return filepath.Join(c.WorkDirectory, "fuzzer")
	default:
		panic(fmt.Sprintf("Unexpected config.FilePath argument (%v)", name))
	}

}
