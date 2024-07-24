package lib

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/shared"

	"github.com/slsa-framework/slsa-verifier/options"
	"github.com/slsa-framework/slsa-verifier/verifiers"
)

var errUserAbortUpdate error = errors.New("update cancelled")

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
	_, _, err := verifiers.VerifyArtifact(ctx, provenance, artifactHash, provenanceOpts, builderOpts)
	return err
}

func checkForDowngrade(currentVersionS, newVersionS string) error {
	currentVersion, err := shared.ParseVersionString(currentVersionS)
	if err != nil {
		return fmt.Errorf("failed to parse current version string: %w", err)
	}
	newVersion, err := shared.ParseVersionString(newVersionS)
	if err != nil {
		return fmt.Errorf("failed to parse new version string: %w", err)
	}
	if currentVersion.GreaterThan(newVersion) {
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
		hctx.GetLogger().Infof("Failed to query SLSA status (err=%#v), assuming that SLSA is currently working", err)
	}
	if err == nil && string(resp) != "OK" {
	slsa_status_error:
		fmt.Printf("SLSA verification is currently experiencing issues:%s\nWhat would you like to do?", string(resp))
		fmt.Println("To abort the update, type 'a'")
		fmt.Println("To continue with the update even though it may fail, type 'c'")
		fmt.Println("To update and skip SLSA validation, type 's'")
		reader := bufio.NewReader(os.Stdin)
		userChoice, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("failed to read response: %v\n", err)
			goto slsa_status_error
		}
		userChoice = strings.TrimSpace(userChoice)
		switch userChoice {
		case "a", "A":
			return errUserAbortUpdate
		case "c", "C":
			// Continue the update and perform SLSA validation even though it may fail
			goto slsa_status_error_continue_update
		case "s", "S":
			// Skip validation and return nil as if the binary passed validation
			return nil
		default:
			fmt.Printf("user selection %#v was not one of 'a', 'c', 's'\n", userChoice)
			goto slsa_status_error
		}
	}
slsa_status_error_continue_update:
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
	if errors.Is(srcErr, errUserAbortUpdate) {
		return srcErr
	}
	fmt.Printf("\nFailed to verify SLSA provenance due to err: %v\nThis is likely due to a SLSA bug (SLSA is a brand new standard, and like all new things, has bugs). Ignoring this failure means falling back to the way most software does updates. Do you want to ignore this failure and update anyways? [y/N]", srcErr)
	reader := bufio.NewReader(os.Stdin)
	resp, err := reader.ReadString('\n')
	if err == nil && strings.TrimSpace(resp) == "y" {
		fmt.Println("Proceeding with update...")
		return nil
	}
	return fmt.Errorf("failed to verify SLSA provenance of the updated binary, aborting update (to bypass, set `export HISHTORY_DISABLE_SLSA_ATTESTATION=true`): %w", srcErr)
}
