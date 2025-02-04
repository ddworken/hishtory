from re import sub
import subprocess
import shutil
import sys 
import os 

ALL_FILES = ['hishtory-linux-amd64', 'hishtory-linux-arm64', 'hishtory-darwin-amd64', 'hishtory-darwin-arm64']

def validate_slsa(hishtory_binary: str) -> None:
    print(f"Validating SLSA attestations with {hishtory_binary=}")
    assert os.path.exists(hishtory_binary)
    assertPresentAndNotAscii(hishtory_binary)
    subprocess.check_output(['chmod', "+x", hishtory_binary])
    for filename in ALL_FILES:
        try:
            print(f"Validating {filename} with {hishtory_binary=}")
            assert os.path.exists(filename)
            slsa_attestation_file = filename + ".intoto.jsonl"
            assert os.path.exists(slsa_attestation_file)
            if "darwin" in filename:
                unsigned_filename = f"{filename}-unsigned"
                assert os.path.exists(unsigned_filename)
                out = subprocess.check_output([
                    hishtory_binary, 
                    "validate-binary",
                    filename, 
                    slsa_attestation_file, 
                    "--is_macos=True", 
                    f"--macos_unsigned_binary={unsigned_filename}"
                ], stderr=subprocess.STDOUT).decode('utf-8')
            else:
                out = subprocess.check_output([
                    hishtory_binary, 
                    "validate-binary", 
                    filename, 
                    slsa_attestation_file
                ], stderr=subprocess.STDOUT).decode('utf-8')
            assert "Verified signature against tlog entry" in out, out
            assert "Verified build using builder" in out, out
        except subprocess.CalledProcessError as e:
            print(f"subprocess.CalledProcessError: stdout={repr(e.stdout)}")
            raise e

def validate_macos_signature(filename: str) -> None:
    assert shutil.which('codesign') is not None 
    out = subprocess.check_output(["codesign", "-dv", "--verbose=4", filename], stderr=subprocess.STDOUT).decode('utf-8')
    print("="*80+f"\nCodesign Output: \n{out}\n\n")
    assert "Authority=Developer ID Application: David Dworken (QUXLNCT7FA)" in out
    assert "Authority=Developer ID Certification Authority" in out
    assert "Authority=Apple Root CA" in out 
    assert "TeamIdentifier=QUXLNCT7FA" in out 

def validate_hishtory_status(filename: str, deep_validation: bool) -> None:
    assert os.path.exists(filename)
    subprocess.check_output(['chmod', "+x", filename])
    status = subprocess.check_output([filename, "status", "-v"]).decode('utf-8')
    if deep_validation:
        git_hash = os.environ['GITHUB_SHA']
        assert git_hash, git_hash
        assert f"Commit Hash: {git_hash}" in status, status 
        assert os.path.exists('VERSION')
        with open('VERSION') as f:
            version = "v0." + f.read().strip()
        assert f"hiSHtory: {version}" in status, status
    else:
        assert "hiSHtory: " in status, status

def assertPresentAndNotAscii(fn):
    if not os.path.exists(fn):
        raise Exception(f"{fn=} does not exist, did it fail to download?")
    out = subprocess.check_output(["file", fn]).decode('utf-8')
    if "ASCII text" in out:
        raise Exception(f"{fn=} is of type {out}")

def main() -> None:
    print("Starting validation of MacOS signatures")
    for filename in ALL_FILES:
        if "darwin" in filename:
            validate_macos_signature(filename)
    print("Starting validation of SLSA attestations")
    validate_slsa("./hishtory-darwin-amd64") 
    validate_slsa(os.path.expanduser("~/.hishtory/hishtory"))
    print("Validating other metadata")
    validate_hishtory_status("./hishtory-darwin-amd64", True)
    validate_hishtory_status(os.path.expanduser("~/.hishtory/hishtory"), False)

if __name__ == '__main__':
    main()