import os 
import requests
import time 
import subprocess

def main():
    version = os.environ['GITHUB_REF'].split('/')[-1].split("-")[0]
    print("Downloading binaries (this may pause for a while)")
    waitUntilPublished(f"https://github.com/ddworken/hishtory/releases/download/{version}-darwin-arm64/hishtory-darwin-arm64", "hishtory-darwin-arm64")
    waitUntilPublished(f"https://github.com/ddworken/hishtory/releases/download/{version}-darwin-amd64/hishtory-darwin-amd64", "hishtory-darwin-amd64")

    print("before sha1sum:")
    os.system("sha1sum hishtory-* 2>&1")

    print("file:")
    os.system("file hishtory-* 2>&1")

    notAscii("hishtory-darwin-arm64")
    notAscii("hishtory-darwin-amd64")

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

    print("after sha1sum:")
    os.system("sha1sum hishtory-* 2>&1")



def notAscii(fn):
    out = subprocess.check_output(["file", fn]).decode('utf-8')
    if "ASCII text" in out:
        raise Exception(f"fn={fn} is of type {out}")

def waitUntilPublished(url, output) -> None:
    startTime = time.time()
    while True:
        r = requests.get(url, headers={'authorization': f'bearer {os.environ["GITHUB_TOKEN"]}'})
        if r.status_code == 200:
            break 
        if (time.time() - startTime)/60 > 20:
            raise Exception(f"failed to get url={url} (startTime={startTime}, endTime={time.time()}), status_code=" + str(r.status_code) + " body=" + str(r.content))
        time.sleep(5)
    with open(output, 'wb') as f:
        f.write(r.content)

if __name__ == '__main__':
    main()