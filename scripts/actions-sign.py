import os 
import requests
import time 
import subprocess

version = os.environ['GITHUB_REF'].split('/')
print("Downloading binaries (this may pause for a while)")
waitUntilPublished(f"https://github.com/ddworken/hishtory/releases/download/{version}-darwin-arm64/hishtory-darwin-arm64", "hishtory-darwin-arm64")
waitUntilPublished(f"https://github.com/ddworken/hishtory/releases/download/{version}-darwin-amd64/hishtory-darwin-amd64", "hishtory-darwin-amd64")

print("sha1sum:")
os.system("sha1sum hishtory-*")

print("file:")
os.system("file hishtory-*")

assert notAscii("hishtory-darwin-arm64")
assert notAscii("hishtory-darwin-amd64")

print("signing...")
os.system("""
echo $MACOS_CERTIFICATE | base64 -d > certificate.p12
security create-keychain -p $MACOS_CERTIFICATE_PWD build.keychain
security default-keychain -s build.keychain
security unlock-keychain -p $MACOS_CERTIFICATE_PWD build.keychain
security import certificate.p12 -k build.keychain -P $MACOS_CERTIFICATE_PWD -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k $MACOS_CERTIFICATE_PWD build.keychain
/usr/bin/codesign --force -s 6D4E1575A0D40C370E294916A8390797106C8A6E hishtory-darwin-arm64 -v
/usr/bin/codesign --force -s 6D4E1575A0D40C370E294916A8390797106C8A6E hishtory-darwin-amd64 -v
""")

def notAscii(fn):
    out = subprocess.check_output(["file", fn]).decode('utf-8')
    if "ASCII text" in out:
        raise Exception(f"fn={fn} is of type {out}")

def waitUntilPublished(url) -> None:
    startTime = time.time()
    while True:
        r = requests.get(url, headers={'authorization': f'bearer {os.environ["GITHUB_TOKEN"]}'})
        if r.status_code == 200:
            break 
        if (time.time() - startTime)/60 > 10:
            raise Exception("failed to get url, status_code=" + str(r.status_code) + " body=" + str(r.content))
        time.sleep(5)
    with open(output, 'wb') as f:
        f.write(r.content)

