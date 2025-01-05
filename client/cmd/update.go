package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"syscall"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"
	"github.com/ddworken/hishtory/shared"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:     "update",
	GroupID: GROUP_ID_INSTALL,
	Short:   "Securely update hishtory to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		lib.CheckFatalError(update(hctx.MakeContext()))
	},
}

var validateBinaryCmd = &cobra.Command{
	Use:    "validate-binary",
	Hidden: true,
	Short:  "[Test Only] Validate the given binary for SLSA compliance",
	Args:   cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		binaryPath := args[0]
		attestationPath := args[1]
		isMacOs, err := cmd.Flags().GetBool("is_macos")
		lib.CheckFatalError(err)
		if isMacOs {
			macOsUnsignedBinaryPath, err := cmd.Flags().GetString("macos_unsigned_binary")
			lib.CheckFatalError(err)
			lib.CheckFatalError(verifyBinaryAgainstUnsignedBinaryForMac(ctx, binaryPath, macOsUnsignedBinaryPath, attestationPath, ""))
		} else {
			lib.CheckFatalError(lib.VerifyBinary(ctx, binaryPath, attestationPath, ""))
		}
	},
}

func GetDownloadData(ctx context.Context) (shared.UpdateInfo, error) {
	respBody, err := lib.ApiGet(ctx, "/api/v1/download")
	if err != nil {
		return shared.UpdateInfo{}, fmt.Errorf("failed to download update info: %w", err)
	}
	var downloadData shared.UpdateInfo
	err = json.Unmarshal(respBody, &downloadData)
	if err != nil {
		return shared.UpdateInfo{}, fmt.Errorf("failed to parse update info: %w", err)
	}
	return downloadData, nil
}

func update(ctx context.Context) error {
	// Download the binary
	downloadData, err := GetDownloadData(ctx)
	if err != nil {
		return err
	}
	if downloadData.Version == "v0."+lib.Version {
		fmt.Printf("Latest version (v0.%s) is already installed\n", lib.Version)
		return nil
	}
	err = downloadFiles(downloadData)
	if err != nil {
		return err
	}

	// Verify the SLSA attestation
	var slsaError error
	if runtime.GOOS == "darwin" {
		slsaError = verifyBinaryMac(ctx, getTmpClientPath(), downloadData)
	} else {
		slsaError = lib.VerifyBinary(ctx, getTmpClientPath(), getTmpClientPath()+".intoto.jsonl", getPossiblyOverriddenVersion(downloadData))
	}
	if slsaError != nil {
		err = lib.HandleSlsaFailure(slsaError)
		if err != nil {
			return err
		}
	}

	// Unlink the existing binary so we can overwrite it even though it is still running
	if runtime.GOOS == "linux" {
		homedir := hctx.GetHome(ctx)
		err = syscall.Unlink(path.Join(homedir, data.GetHishtoryPath(), "hishtory"))
		if err != nil {
			return fmt.Errorf("failed to unlink %s for update: %w", path.Join(homedir, data.GetHishtoryPath(), "hishtory"), err)
		}
	}

	// Install the new one
	cmd := exec.Command("chmod", "+x", getTmpClientPath())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to chmod +x the update (stdout=%#v, stderr=%#v): %w", stdout.String(), stderr.String(), err)
	}
	cmd = exec.Command(getTmpClientPath(), "install", "--skip-update-config-modification", "--currently-installed-version", "v0."+lib.Version)
	cmd.Stdout = os.Stdout
	stderr = bytes.Buffer{}
	cmd.Stdin = os.Stdin
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to install update (stderr=%#v), is %s in a noexec directory? (if so, set the TMPDIR environment variable): %w", stderr.String(), getTmpClientPath(), err)
	}
	fmt.Printf("Successfully updated hishtory from v0.%s to %s\n", lib.Version, getPossiblyOverriddenVersion(downloadData))

	// Delete the file after installing to prevent issues like #227
	_ = os.Remove(getTmpClientPath())
	return nil
}

func verifyBinaryMac(ctx context.Context, binaryPath string, downloadData shared.UpdateInfo) error {
	// On Mac, binary verification is a bit more complicated since mac binaries are code
	// signed. To verify a signed binary, we:
	// 1. Download the unsigned binary
	// 2. Strip the real signature from the signed binary and the ad-hoc signature from the unsigned binary
	// 3. Assert that those binaries match
	// 4. Use SLSA to verify the unsigned binary (pre-strip)
	// Yes, this is complicated. But AFAICT, it is the only solution here.

	// Step 1: Download the "unsigned" binary that actually has an ad-hoc signature from the
	// go compiler.
	unsignedBinaryPath := binaryPath + "-unsigned"
	var err error = nil
	unsignedUrl := ""
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		unsignedUrl = downloadData.DarwinAmd64UnsignedUrl
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		unsignedUrl = downloadData.DarwinArm64UnsignedUrl
	} else {
		return fmt.Errorf("verifyBinaryMac() called for the unhandled branch GOOS=%s, GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	}
	if forcedVersion := os.Getenv("HISHTORY_FORCE_CLIENT_VERSION"); forcedVersion != "" {
		unsignedUrl = strings.ReplaceAll(unsignedUrl, downloadData.Version, forcedVersion)
	}

	err = downloadFile(unsignedBinaryPath, unsignedUrl)
	if err != nil {
		return err
	}

	// Step 2, 3, and 4 in this function:
	return verifyBinaryAgainstUnsignedBinaryForMac(ctx, binaryPath, unsignedBinaryPath, getTmpClientPath()+".intoto.jsonl", getPossiblyOverriddenVersion(downloadData))
}

func verifyBinaryAgainstUnsignedBinaryForMac(ctx context.Context, binaryPath, unsignedBinaryPath, attestationPath, version string) error {
	// Step 2: Create the .nosig files that have no signatures whatsoever
	noSigSuffix := ".nosig"
	err := stripCodeSignature(binaryPath, binaryPath+noSigSuffix)
	if err != nil {
		return err
	}
	err = stripCodeSignature(unsignedBinaryPath, unsignedBinaryPath+noSigSuffix)
	if err != nil {
		return err
	}

	// Step 3: Compare the binaries
	err = assertIdenticalBinaries(binaryPath+noSigSuffix, unsignedBinaryPath+noSigSuffix)
	if err != nil {
		return err
	}

	// Step 4: Use SLSA to verify the unsigned binary
	return lib.VerifyBinary(ctx, unsignedBinaryPath, attestationPath, version)
}

func assertIdenticalBinaries(bin1Path, bin2Path string) error {
	bin1, err := os.ReadFile(bin1Path)
	if err != nil {
		return err
	}
	bin2, err := os.ReadFile(bin2Path)
	if err != nil {
		return err
	}
	if len(bin1) != len(bin2) {
		return fmt.Errorf("unsigned binaries have different lengths (len(%s)=%d, len(%s)=%d)", bin1Path, len(bin1), bin2Path, len(bin2))
	}
	differences := make([]string, 0)
	for i := range bin1 {
		b1 := bin1[i]
		b2 := bin2[i]
		if b1 != b2 {
			differences = append(differences, fmt.Sprintf("diff at index %d: %s[%d]=%x, %s[%d]=%x", i, bin1Path, i, b1, bin2Path, i, b2))
		}
	}
	for _, d := range differences {
		hctx.GetLogger().Infof("comparing binaries: %#v\n", d)
	}
	if len(differences) > 5 {
		return fmt.Errorf("found %d differences in the binary", len(differences))
	}
	return nil
}

func stripCodeSignature(inPath, outPath string) error {
	_, err := exec.LookPath("codesign_allocate")
	if err != nil {
		return fmt.Errorf("your system is missing the codesign_allocate tool, so we can't verify the SLSA attestation (you can bypass this by setting `export HISHTORY_DISABLE_SLSA_ATTESTATION=true` in your shell)")
	}
	cmd := exec.Command("codesign_allocate", "-i", inPath, "-o", outPath, "-r")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to use codesign_allocate to strip signatures on binary=%v (stdout=%#v, stderr%#v): %w", inPath, stdout.String(), stderr.String(), err)
	}
	return nil
}

func downloadFiles(updateInfo shared.UpdateInfo) error {
	clientUrl := ""
	clientProvenanceUrl := ""
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		clientUrl = updateInfo.LinuxAmd64Url
		clientProvenanceUrl = updateInfo.LinuxAmd64AttestationUrl
	} else if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		clientUrl = updateInfo.LinuxArm64Url
		clientProvenanceUrl = updateInfo.LinuxArm64AttestationUrl
	} else if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		clientUrl = updateInfo.LinuxArm7Url
		clientProvenanceUrl = updateInfo.LinuxArm7AttestationUrl
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		clientUrl = updateInfo.DarwinAmd64Url
		clientProvenanceUrl = updateInfo.DarwinAmd64AttestationUrl
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		clientUrl = updateInfo.DarwinArm64Url
		clientProvenanceUrl = updateInfo.DarwinArm64AttestationUrl
	} else {
		return fmt.Errorf("no update info found for GOOS=%s, GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	}
	if forcedVersion := os.Getenv("HISHTORY_FORCE_CLIENT_VERSION"); forcedVersion != "" {
		clientUrl = strings.ReplaceAll(clientUrl, updateInfo.Version, forcedVersion)
		clientProvenanceUrl = strings.ReplaceAll(clientProvenanceUrl, updateInfo.Version, forcedVersion)
	}
	err := downloadFile(getTmpClientPath(), clientUrl)
	if err != nil {
		return err
	}
	err = downloadFile(getTmpClientPath()+".intoto.jsonl", clientProvenanceUrl)
	if err != nil {
		return err
	}
	return nil
}

func getPossiblyOverriddenVersion(updateInfo shared.UpdateInfo) string {
	if forcedVersion := os.Getenv("HISHTORY_FORCE_CLIENT_VERSION"); forcedVersion != "" {
		return forcedVersion
	}
	return updateInfo.Version
}

func getTmpClientPath() string {
	tmpDir := "/tmp/"
	if os.Getenv("TMPDIR") != "" {
		tmpDir = os.Getenv("TMPDIR")
	}
	return path.Join(tmpDir, "hishtory-client")
}

func downloadFile(filename, url string) error {
	// Support simulating network errors for the purposes of testing
	if os.Getenv("HISHTORY_SIMULATE_NETWORK_ERROR") != "" {
		return fmt.Errorf("simulated network error: dial tcp: lookup api.hishtory.dev")
	}

	// Download the data
	resp, err := lib.GetHttpClient().Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file at %s to %s: %w", url, filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download file at %s due to resp_code=%d", url, resp.StatusCode)
	}

	// Delete the file if it already exists. This is necessary due to https://openradar.appspot.com/FB8735191
	if _, err := os.Stat(filename); err == nil {
		err = os.Remove(filename)
		if err != nil {
			return fmt.Errorf("failed to delete file %v when trying to download a new version", filename)
		}
	}

	// Create the file
	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to save file to %s: %w", filename, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)

	return err
}

func init() {
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(validateBinaryCmd)
	validateBinaryCmd.PersistentFlags().Bool("is_macos", false, "Whether the binary we are validating is for MacOS")
	validateBinaryCmd.PersistentFlags().String("macos_unsigned_binary", "", "The path to the unsigned MacOS binary, if is_macos=true")
}
