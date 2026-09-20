package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultFontSize = 10
	MinFontSize     = 8
	MaxFontSize     = 48
)

type InstallConfig struct {
	EnableWanxiang bool
	FontSize       int
	Yes            bool
	DryRun         bool
	Verbose        bool
	WorkspaceDir   string
}

type DetectedEnv struct {
	HomeDir              string
	SessionType          string
	Desktop              string
	IsKDE                bool
	AURHelper            string
	DialogAvailable      bool
	FingerprintAvailable bool
	OctagramPluginPath   string
	HasOctagramPlugin    bool
	EnvironmentFilePath  string
}

type ReleaseInfo struct {
	Owner     string
	Repo      string
	Tag       string
	AssetName string
	Digest    string
	URL       string
}

type DeploymentResult struct {
	BackupPaths []string
	TargetPaths []string
}

func DefaultInstallConfig() InstallConfig {
	home, _ := os.UserHomeDir()
	return InstallConfig{
		EnableWanxiang: true,
		FontSize:       DefaultFontSize,
		WorkspaceDir:   filepath.Join(home, ".cache", "rime-ice-installer"),
	}
}
