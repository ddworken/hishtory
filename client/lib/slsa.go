package lib

import (
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

func verify(ctx *context.Context, provenance []byte, artifactHash, source, branch, versionTag string) error {
	provenanceOpts := &options.ProvenanceOpts{
		ExpectedSourceURI:    source,
		ExpectedBranch:       &branch,
		ExpectedDigest:       artifactHash,
		ExpectedVersionedTag: &versionTag,
	}
	builderOpts := &options.BuilderOpts{}
	_, _, err := verifiers.Verify(context.TODO(), provenance, artifactHash, provenanceOpts, builderOpts)
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
	if currentVersion > newVersion {
		return fmt.Errorf("failed to update because the new version (%#v) is a downgrade compared to the current version (%#v)", newVersionS, currentVersionS)
	}
	return nil
}

func verifyBinary(ctx *context.Context, binaryPath, attestationPath, versionTag string) error {
	if os.Getenv("HISHTORY_DISABLE_SLSA_ATTESTATION") == "true" {
		return nil
	}

	if err := checkForDowngrade(Version, versionTag); err != nil && os.Getenv("HISHTORY_ALLOW_DOWNGRADE") == "true" {
		return err
	}

	attestation, err := os.ReadFile(attestationPath)
	if err != nil {
		return fmt.Errorf("failed to read attestation file: %v", err)
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
		return "", fmt.Errorf("failed to read binary for verification purposes: %v", err)
	}
	defer binaryFile.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, binaryFile); err != nil {
		return "", fmt.Errorf("failed to hash binary: %v", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	return hash, nil
}
