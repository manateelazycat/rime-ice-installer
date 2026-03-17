package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"rime-ice-installer/internal/config"
)

const octagramPluginPath = "/usr/lib/rime-plugins/librime-octagram.so"

func Detect() (config.DetectedEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config.DetectedEnv{}, err
	}

	sessionType := strings.ToLower(strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE")))
	desktopRaw := strings.ToLower(strings.Join([]string{
		os.Getenv("XDG_CURRENT_DESKTOP"),
		os.Getenv("XDG_SESSION_DESKTOP"),
		os.Getenv("DESKTOP_SESSION"),
	}, " "))

	isKDE := strings.Contains(desktopRaw, "kde") || strings.Contains(desktopRaw, "plasma")
	aurHelper := detectAURHelper()
	dialogAvailable := CommandExists("dialog")
	hasOctagram := fileExists(octagramPluginPath)

	detected := config.DetectedEnv{
		HomeDir:             home,
		SessionType:         sessionType,
		Desktop:             desktopRaw,
		IsKDE:               isKDE,
		AURHelper:           aurHelper,
		DialogAvailable:     dialogAvailable,
		OctagramPluginPath:  octagramPluginPath,
		HasOctagramPlugin:   hasOctagram,
		EnvironmentFilePath: SuggestedEnvFile(home, sessionType, isKDE),
	}
	return detected, nil
}

func detectAURHelper() string {
	for _, candidate := range []string{"yay", "paru"} {
		if CommandExists(candidate) {
			return candidate
		}
	}
	return ""
}

func RequiredCommands(interactive bool) []string {
	commands := []string{"sudo", "pacman", "git", "curl", "unzip", "gdbus"}
	if interactive {
		commands = append(commands, "dialog")
	}
	return commands
}

func MissingCommands(commands []string) []string {
	missing := make([]string, 0)
	for _, command := range commands {
		if !CommandExists(command) {
			missing = append(missing, command)
		}
	}
	return missing
}

func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func SuggestedEnvFile(home, sessionType string, isKDE bool) string {
	if sessionType == "wayland" && isKDE {
		return filepath.Join(home, ".config", "plasma-workspace", "env", "ime.sh")
	}
	return filepath.Join(home, ".xprofile")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
