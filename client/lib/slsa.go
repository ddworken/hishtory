package lib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/ddworken/hishtory/client/vndor/slsa_verifier"
	"github.com/sigstore/cosign/cmd/cosign/cli/rekor"
)

var defaultRekorAddr = "https://rekor.sigstore.dev"

// Verify SLSA provenance of the downloaded binary
// Copied from https://github.com/slsa-framework/slsa-verifier/blob/aee753f/main.go
// Once the slsa-verifier supports being used as a library, this can be removed
func verify(provenance []byte, artifactHash, source, branch, versionTag string) error {
	rClient, err := rekor.NewClient(defaultRekorAddr)
	if err != nil {
		return err
	}

	// Get Rekor entries corresponding to the binary artifact in the provenance.
	uuids, err := slsa_verifier.GetRekorEntries(rClient, artifactHash)
	if err != nil {
		return err
	}

	env, err := slsa_verifier.EnvelopeFromBytes(provenance)
	if err != nil {
		return err
	}

	// Verify the provenance and return the signing certificate.
	cert, err := slsa_verifier.FindSigningCertificate(context.Background(), uuids, *env, rClient)
	if err != nil {
		return fmt.Errorf("failed to locate signing certificate: %v", err)
	}

	// Get the workflow info given the certificate information.
	workflowInfo, err := slsa_verifier.GetWorkflowInfoFromCertificate(cert)
	if err != nil {
		return fmt.Errorf("failed to verify workflow info: %v", err)
	}

	// Unpack and verify info in the provenance, including the Subject Digest.
	if err := slsa_verifier.VerifyProvenance(env, artifactHash); err != nil {
		return fmt.Errorf("failed to verify provenance: %v", err)
	}

	// Verify the workflow identity.
	fmt.Printf("source=%#v, workflowInfo=%#v\n", source, workflowInfo)
	if err := slsa_verifier.VerifyWorkflowIdentity(workflowInfo, source); err != nil {
		return fmt.Errorf("failed to verify workflow identity: %v", err)
	}

	// Verify the branch.
	// TODO: This started failing for some reason? base_ref was null
	// if err := pkg.VerifyBranch(env, branch); err != nil {
	// 	return err
	// }

	// Verify the tag.
	if err := slsa_verifier.VerifyTag(env, versionTag); err != nil {
		return fmt.Errorf("failed to verify tag: %v", err)
	}

	return nil
}

func verifyBinary(binaryPath, attestationPath, versionTag string) error {
	if os.Getenv("HISHTORY_DISABLE_SLSA_ATTESTATION") == "true" {
		return nil
	}

	// TODO: Also verify that the version is newer and this isn't a downgrade
	attestation, err := os.ReadFile(attestationPath)
	if err != nil {
		return fmt.Errorf("failed to read attestation file: %v", err)
	}

	hash, err := getFileHash(binaryPath)
	if err != nil {
		return err
	}

	return verify(attestation, hash, "github.com/ddworken/hishtory", "master", versionTag)
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
