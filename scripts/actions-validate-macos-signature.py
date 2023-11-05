import subprocess
import shutil
import sys 

def main():
    assert shutil.which('codesign') is not None 
    out = subprocess.check_output(["codesign", "-dv", "--verbose=4", sys.argv[1]], stderr=subprocess.STDOUT).decode('utf-8')
    print("="*80+f"\nCodesign Output: \n{out}\n\n")
    assert "Authority=Developer ID Application: David Dworken (QUXLNCT7FA)" in out
    assert "Authority=Developer ID Certification Authority" in out
    assert "Authority=Apple Root CA" in out 
    assert "TeamIdentifier=QUXLNCT7FA" in out 

if __name__ == '__main__':
    main()