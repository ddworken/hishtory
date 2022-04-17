package lib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/sigstore/cosign/cmd/cosign/cli/rekor"
	"github.com/slsa-framework/slsa-verifier/pkg"
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
	uuids, err := pkg.GetRekorEntries(rClient, artifactHash)
	if err != nil {
		return err
	}

	env, err := pkg.EnvelopeFromBytes(provenance)
	if err != nil {
		return err
	}

	// Verify the provenance and return the signing certificate.
	cert, err := pkg.FindSigningCertificate(context.Background(), uuids, *env, rClient)
	if err != nil {
		return err
	}

	// Get the workflow info given the certificate information.
	workflowInfo, err := pkg.GetWorkflowInfoFromCertificate(cert)
	if err != nil {
		return err
	}

	// Unpack and verify info in the provenance, including the Subject Digest.
	if err := pkg.VerifyProvenance(env, artifactHash); err != nil {
		return err
	}

	// Verify the workflow identity.
	if err := pkg.VerifyWorkflowIdentity(workflowInfo, source); err != nil {
		return err
	}

	// Verify the branch.
	if err := pkg.VerifyBranch(env, branch); err != nil {
		return err
	}

	// Verify the tag.
	if err := pkg.VerifyTag(env, versionTag); err != nil {
		return err
	}

	// TODO
	// Verify the versioned tag.
	// if versiontag != nil {
	// 	if err := pkg.VerifyVersionedTag(env, *versiontag); err != nil {
	// 		return err
	// 	}
	// }

	return nil
}

func verifyBinary(binaryPath, attestationPath, versionTag string) error {
	// TODO: Also verify that the version is newer and this isn't a downgrade
	attestation, err := os.ReadFile(attestationPath)
	if err != nil {
		return fmt.Errorf("failed to read attestation file: %v", err)
	}

	binaryFile, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read binary for verification purposes: %v", err)
	}
	defer binaryFile.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, binaryFile); err != nil {
		return fmt.Errorf("failed to hash binary: %v", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))

	return verify(attestation, hash, "github.com/ddworken/hishtory", "master", versionTag)
}
