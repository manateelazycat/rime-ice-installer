package env

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
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
	fingerprintAvailable := detectFingerprintAvailable()
	hasOctagram := fileExists(octagramPluginPath)

	detected := config.DetectedEnv{
		HomeDir:              home,
		SessionType:          sessionType,
		Desktop:              desktopRaw,
		IsKDE:                isKDE,
		AURHelper:            aurHelper,
		DialogAvailable:      dialogAvailable,
		FingerprintAvailable: fingerprintAvailable,
		OctagramPluginPath:   octagramPluginPath,
		HasOctagramPlugin:    hasOctagram,
		EnvironmentFilePath:  SuggestedEnvFile(home, sessionType, isKDE),
	}
	return detected, nil
}

var (
	dbusObjectPathPattern = regexp.MustCompile(`objectpath '([^']+)'`)
	enrolledFingerPattern = regexp.MustCompile(`'(?:left|right)-(?:thumb|index-finger|middle-finger|ring-finger|little-finger)'`)
)

func detectFingerprintAvailable() bool {
	if !CommandExists("gdbus") || !sudoPAMUsesFingerprint("/etc/pam.d/sudo", map[string]bool{}) {
		return false
	}

	currentUser, err := user.Current()
	if err != nil || strings.TrimSpace(currentUser.Username) == "" {
		return false
	}
	username := currentUser.Username

	managerOutput, err := exec.Command(
		"gdbus", "call", "--system",
		"--dest", "net.reactivated.Fprint",
		"--object-path", "/net/reactivated/Fprint/Manager",
		"--method", "net.reactivated.Fprint.Manager.GetDefaultDevice",
	).Output()
	if err != nil {
		return false
	}
	devicePath := defaultFingerprintDevice(managerOutput)
	if devicePath == "" {
		return false
	}

	fingersOutput, err := exec.Command(
		"gdbus", "call", "--system",
		"--dest", "net.reactivated.Fprint",
		"--object-path", devicePath,
		"--method", "net.reactivated.Fprint.Device.ListEnrolledFingers",
		username,
	).Output()
	return err == nil && hasEnrolledFingerprint(fingersOutput)
}

func defaultFingerprintDevice(output []byte) string {
	match := dbusObjectPathPattern.FindSubmatch(output)
	if len(match) != 2 {
		return ""
	}
	return string(match[1])
}

func hasEnrolledFingerprint(output []byte) bool {
	return enrolledFingerPattern.Match(output)
}

func sudoPAMUsesFingerprint(path string, visited map[string]bool) bool {
	if visited[path] {
		return false
	}
	visited[path] = true

	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.TrimPrefix(fields[0], "-") != "auth" {
			continue
		}
		if strings.Contains(line, "pam_fprintd.so") {
			return true
		}
		if fields[1] == "include" || fields[1] == "substack" {
			if sudoPAMUsesFingerprint(filepath.Join(filepath.Dir(path), fields[2]), visited) {
				return true
			}
		}
	}
	return false
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
