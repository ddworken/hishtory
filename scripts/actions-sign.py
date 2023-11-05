import os 
import requests
import time 
import subprocess

def main():
    print("before sha1sum:")
    os.system("sha1sum hishtory-* 2>&1")

    print("file:")
    os.system("file hishtory-* 2>&1")

    assertPresentAndNotAscii("hishtory-darwin-arm64")
    assertPresentAndNotAscii("hishtory-darwin-amd64")

    # TODO: Update this file to fail if the input files don't exist

    print("signing...")
    os.system("""
    set -emo pipefail
    cp hishtory-darwin-arm64 hishtory-darwin-arm64-unsigned
    cp hishtory-darwin-amd64 hishtory-darwin-amd64-unsigned
    echo $MACOS_CERTIFICATE | base64 -d > certificate.p12
    security create-keychain -p $MACOS_CERTIFICATE_PWD build.keychain
    security default-keychain -s build.keychain
    security unlock-keychain -p $MACOS_CERTIFICATE_PWD build.keychain
    security import certificate.p12 -k build.keychain -P $MACOS_CERTIFICATE_PWD -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k $MACOS_CERTIFICATE_PWD build.keychain
    /usr/bin/codesign --force -s 6D4E1575A0D40C370E294916A8390797106C8A6E hishtory-darwin-arm64 -v
    /usr/bin/codesign --force -s 6D4E1575A0D40C370E294916A8390797106C8A6E hishtory-darwin-amd64 -v
    """)

    print("after sha1sum:")
    os.system("sha1sum hishtory-* 2>&1")



def assertPresentAndNotAscii(fn):
    if not os.path.exists(fn):
        raise Exception(f"{fn=} does not exist, did it fail to download?")
    out = subprocess.check_output(["file", fn]).decode('utf-8')
    if "ASCII text" in out:
        raise Exception(f"{fn=} is of type {out}")

if __name__ == '__main__':
    main()