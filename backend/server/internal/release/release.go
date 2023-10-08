package release

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ddworken/hishtory/shared"
)

// TODO: Can we get rid of this bit of mutable state by changing UpdateReleaseVersion to return the latest version?
var Version = "UNKNOWN"

type releaseInfo struct {
	Name string `json:"name"`
}

const releaseURL = "https://api.github.com/repos/ddworken/hishtory/releases/latest"

func UpdateReleaseVersion() error {
	resp, err := http.Get(releaseURL)
	if err != nil {
		return fmt.Errorf("failed to get latest release version: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read github API response body: %w", err)
	}
	if resp.StatusCode == 403 && strings.Contains(string(respBody), "API rate limit exceeded for ") {
		fmt.Printf("failed to update release version due to 403 err, body=%#v\n", string(respBody))
		return nil
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to call github API, status_code=%d, body=%#v", resp.StatusCode, string(respBody))
	}
	var info releaseInfo
	err = json.Unmarshal(respBody, &info)
	if err != nil {
		return fmt.Errorf("failed to parse github API response: %w", err)
	}
	latestVersionTag := info.Name
	Version = decrementVersionIfInvalid(latestVersionTag)
	return nil
}

func BuildUpdateInfo(version string) shared.UpdateInfo {
	return shared.UpdateInfo{
		LinuxAmd64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64", version),
		LinuxAmd64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-amd64.intoto.jsonl", version),
		LinuxArm64Url:             fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64", version),
		LinuxArm64AttestationUrl:  fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm64.intoto.jsonl", version),
		LinuxArm7Url:              fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm", version),
		LinuxArm7AttestationUrl:   fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-linux-arm.intoto.jsonl", version),
		DarwinAmd64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64", version),
		DarwinAmd64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64-unsigned", version),
		DarwinAmd64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-amd64.intoto.jsonl", version),
		DarwinArm64Url:            fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64", version),
		DarwinArm64UnsignedUrl:    fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64-unsigned", version),
		DarwinArm64AttestationUrl: fmt.Sprintf("https://github.com/ddworken/hishtory/releases/download/%s/hishtory-darwin-arm64.intoto.jsonl", version),
		Version:                   version,
	}
}

func decrementVersionIfInvalid(initialVersion string) string {
	// Decrements the version up to 5 times if the version doesn't have valid binaries yet.
	version := initialVersion
	for i := 0; i < 5; i++ {
		updateInfo := BuildUpdateInfo(version)
		err := assertValidUpdate(updateInfo)
		if err == nil {
			fmt.Printf("Found a valid version: %v\n", version)
			return version
		}
		fmt.Printf("Found %s to be an invalid version: %v\n", version, err)
		version, err = decrementVersion(version)
		if err != nil {
			fmt.Printf("Failed to decrement version after finding the latest version was invalid: %v\n", err)
			return initialVersion
		}
	}
	fmt.Printf("Decremented the version 5 times and failed to find a valid version version number, initial version number: %v, last checked version number: %v\n", initialVersion, version)
	return initialVersion
}

func assertValidUpdate(updateInfo shared.UpdateInfo) error {
	urls := []string{
		updateInfo.LinuxAmd64Url,
		updateInfo.LinuxAmd64AttestationUrl,
		updateInfo.LinuxArm64Url,
		updateInfo.LinuxArm64AttestationUrl,
		updateInfo.LinuxArm7Url,
		updateInfo.LinuxArm7AttestationUrl,
		updateInfo.DarwinAmd64Url,
		updateInfo.DarwinAmd64UnsignedUrl,
		updateInfo.DarwinAmd64AttestationUrl,
		updateInfo.DarwinArm64Url,
		updateInfo.DarwinArm64UnsignedUrl,
		updateInfo.DarwinArm64AttestationUrl,
	}
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("failed to retrieve URL %#v: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("URL %#v returned 404", url)
		}
	}
	return nil
}

func decrementVersion(version string) (string, error) {
	if version == "UNKNOWN" {
		return "", fmt.Errorf("cannot decrement UNKNOWN")
	}
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	versionNumber, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid version: %s", version)
	}
	return parts[0] + "." + strconv.Itoa(versionNumber-1), nil
}
