package lib

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/slsa-framework/slsa-verifier/options"
	"github.com/slsa-framework/slsa-verifier/verifiers"
)

func verify(ctx context.Context, provenance []byte, artifactHash, source, branch, versionTag string) error {
	provenanceOpts := &options.ProvenanceOpts{
		ExpectedSourceURI: source,
		ExpectedBranch:    &branch,
		ExpectedDigest:    artifactHash,
	}
	if versionTag != "" {
		provenanceOpts.ExpectedVersionedTag = &versionTag
	}
	builderOpts := &options.BuilderOpts{}
	_, _, err := verifiers.Verify(ctx, provenance, artifactHash, provenanceOpts, builderOpts)
	return err
}

func checkForDowngrade(currentVersionS, newVersionS string) error {
	currentVersion, err := strconv.Atoi(strings.TrimPrefix(currentVersionS, "v0."))
	if err != nil {
		return fmt.Errorf("failed to parse current version %#v", currentVersionS)
	}
	newVersion, err := strconv.Atoi(strings.TrimPrefix(newVersionS, "v0."))
	if err != nil {
		return fmt.Errorf("failed to parse updated version %#v", newVersionS)
	}
	// TODO: migrate this to the version parser struct
	if currentVersion > newVersion {
		return fmt.Errorf("failed to update because the new version (%#v) is a downgrade compared to the current version (%#v)", newVersionS, currentVersionS)
	}
	return nil
}

func VerifyBinary(ctx context.Context, binaryPath, attestationPath, versionTag string) error {
	if os.Getenv("HISHTORY_DISABLE_SLSA_ATTESTATION") == "true" {
		return nil
	}
	resp, err := ApiGet(ctx, "/api/v1/slsa-status?newVersion="+versionTag)
	if err != nil {
		return nil
	}
	if string(resp) != "OK" {
		fmt.Printf("SLSA verification is currently broken (%s), skipping SLSA validation...\n", string(resp))
		return nil
	}

	if err := checkForDowngrade(Version, versionTag); err != nil && os.Getenv("HISHTORY_ALLOW_DOWNGRADE") == "true" {
		return err
	}

	attestation, err := os.ReadFile(attestationPath)
	if err != nil {
		return fmt.Errorf("failed to read attestation file: %w", err)
	}

	hash, err := getFileHash(binaryPath)
	if err != nil {
		return err
	}

	return verify(ctx, attestation, hash, "github.com/ddworken/hishtory", "master", versionTag)
}

func getFileHash(binaryPath string) (string, error) {
	binaryFile, err := os.Open(binaryPath)
	if err != nil {
		return "", fmt.Errorf("failed to read binary for verification purposes: %w", err)
	}
	defer binaryFile.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, binaryFile); err != nil {
		return "", fmt.Errorf("failed to hash binary: %w", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	return hash, nil
}

func HandleSlsaFailure(srcErr error) error {
	fmt.Printf("\nFailed to verify SLSA provenance! This is likely due to a SLSA bug (SLSA is a brand new standard, and like all new things, has bugs). Ignoring this failure means falling back to the way most software does updates. Do you want to ignore this failure and update anyways? [y/N]")
	reader := bufio.NewReader(os.Stdin)
	resp, err := reader.ReadString('\n')
	if err == nil && strings.TrimSpace(resp) == "y" {
		fmt.Println("Proceeding with update...")
		return nil
	}
	return fmt.Errorf("failed to verify SLSA provenance of the updated binary, aborting update (to bypass, set `export HISHTORY_DISABLE_SLSA_ATTESTATION=true`): %w", srcErr)
}
