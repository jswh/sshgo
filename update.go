package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	githubRepo = "jswh/sshgo"
)

// getUpdateServer returns the base URL for update checks.
// Override with SSHGO_UPDATE_SERVER environment variable for testing.
func getUpdateServer() string {
	if s := os.Getenv("SSHGO_UPDATE_SERVER"); s != "" {
		return s
	}
	return "https://github.com/" + githubRepo
}

// getLatestURL returns the URL to fetch the latest release tag.
func getLatestURL() string {
	return getUpdateServer() + "/releases/latest"
}

// getDownloadURL returns the download URL for a specific release.
func getDownloadURL(tag string) string {
	return fmt.Sprintf("%s/releases/download/%s/sshgo-%s-%s",
		getUpdateServer(), tag, runtime.GOOS, runtime.GOARCH)
}

func handleUpdate(args []string) {
	if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: sshgo update")
		fmt.Println()
		fmt.Println("Check for updates and replace the current binary with the latest release.")
		fmt.Println("The latest version is fetched from GitHub releases.")
		fmt.Println()
		fmt.Println("Environment:")
		fmt.Println("  SSHGO_UPDATE_SERVER  Override the update server URL (for testing)")
		return
	}

	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "Error: unknown flag %q\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: sshgo update")
		os.Exit(1)
	}

	currentVersion := version
	latestVersion, err := getLatestReleaseTag()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	downloadURL := getDownloadURL(latestVersion)

	if currentVersion != "dev" {
		cmp := semverCompare(currentVersion, latestVersion)
		if cmp >= 0 {
			fmt.Printf("Already up to date (%s)\n", currentVersion)
			return
		}
		fmt.Printf("Updating from %s to %s ...\n", currentVersion, latestVersion)
	} else {
		fmt.Printf("Development build detected. Downloading latest release %s ...\n", latestVersion)
	}

	// Download to a temp file
	tmpFile, err := os.CreateTemp("", "sshgo-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	fmt.Printf("Downloading %s ...\n", downloadURL)
	if err := downloadFile(downloadURL, tmpFile); err != nil {
		tmpFile.Close()
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}
	tmpFile.Close()

	// Mark as executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set permissions: %v\n", err)
		os.Exit(1)
	}

	// Get current binary path
	currentExe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to determine current executable path: %v\n", err)
		os.Exit(1)
	}
	resolvedExe, err := filepath.EvalSymlinks(currentExe)
	if err == nil {
		currentExe = resolvedExe
	}

	// Replace binary
	if err := replaceBinary(tmpPath, currentExe); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to replace binary: %v\n", err)
		fmt.Printf("\nDownload saved to: %s\n", tmpPath)
		fmt.Printf("You can manually install it:\n")
		fmt.Printf("  sudo mv %s %s\n", tmpPath, currentExe)
		fmt.Printf("  sudo chmod +x %s\n", currentExe)
		os.Exit(1)
	}

	fmt.Printf("Successfully updated to %s\n", latestVersion)
}

// getLatestReleaseTag follows the /releases/latest redirect to extract the
// latest release tag. This avoids GitHub API rate limits.
func getLatestReleaseTag() (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(getLatestURL())
	if err != nil {
		return "", fmt.Errorf("failed to reach update server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("unexpected response from update server: %s", resp.Status)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no redirect location found")
	}

	return parseTagFromLocation(location)
}

// parseTagFromLocation extracts the version tag from a GitHub-style redirect
// Location header.
//
// Accepted formats:
//   /jswh/sshgo/releases/tag/v1.2.3       (relative)
//   /releases/tag/v0.0.1                   (mock server)
//   https://github.com/jswh/sshgo/releases/tag/v1.2.3  (absolute)
func parseTagFromLocation(location string) (string, error) {
	const marker = "/releases/tag/"
	idx := strings.Index(location, marker)
	if idx < 0 {
		return "", fmt.Errorf("unexpected redirect location: %s", location)
	}
	tag := location[idx+len(marker):]
	// Remove any trailing path segments or query parameters
	if i := strings.IndexAny(tag, "/?"); i >= 0 {
		tag = tag[:i]
	}
	if tag == "" {
		return "", fmt.Errorf("empty tag in redirect location: %s", location)
	}
	return tag, nil
}

// downloadFile downloads from url to the given file.
func downloadFile(url string, file *os.File) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %s", resp.Status)
	}

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("download interrupted: %w", err)
	}
	if written == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	return nil
}

// replaceBinary moves tmpPath to targetPath, falling back to sudo if
// permission is denied. On success targetPath is replaced atomically
// (on the same filesystem).
func replaceBinary(tmpPath, targetPath string) error {
	// Try direct rename first (works when both are on same filesystem
	// and we have write permission to targetPath's directory)
	if err := os.Rename(tmpPath, targetPath); err == nil {
		return nil
	}

	// If Rename failed (e.g. cross-device), fall through to copy + remove
	// First try copy
	src, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("cannot open downloaded file: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0755)
	if err == nil {
		_, copyErr := io.Copy(dst, src)
		dst.Close()
		if copyErr == nil {
			os.Remove(tmpPath)
			return nil
		}
	}

	// If direct write fails, try sudo mv
	cmd := exec.Command("sudo", "mv", tmpPath, targetPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo mv failed: %w", err)
	}

	// Ensure correct permissions after sudo mv
	exec.Command("sudo", "chmod", "755", targetPath).Run()

	return nil
}

// semverCompare compares two semver strings (with optional "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func semverCompare(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			na, err := strconv.Atoi(partsA[i])
			if err != nil {
				return 0
			}
			numA = na
		}
		if i < len(partsB) {
			nb, err := strconv.Atoi(partsB[i])
			if err != nil {
				return 0
			}
			numB = nb
		}
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}
