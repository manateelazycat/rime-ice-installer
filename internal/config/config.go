package config

import (
	"os"
	"path/filepath"
)

type InstallConfig struct {
	EnableWanxiang bool
	Yes            bool
	DryRun         bool
	Verbose        bool
	WorkspaceDir   string
}

type DetectedEnv struct {
	HomeDir             string
	SessionType         string
	Desktop             string
	IsKDE               bool
	AURHelper           string
	DialogAvailable     bool
	OctagramPluginPath  string
	HasOctagramPlugin   bool
	EnvironmentFilePath string
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
		WorkspaceDir:   filepath.Join(home, ".cache", "rime-ice-installer"),
	}
}
