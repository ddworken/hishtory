"""
A small install script to download the correct hishtory binary for the current OS/architecture.
The hishtory binary is in charge of installing itself, this just downloads the correct binary and
executes it.
"""

import json
import urllib.request
import platform
import sys
import os

with urllib.request.urlopen('https://api.hishtory.dev/api/v1/download') as response:
    resp_body = response.read()
download_options = json.loads(resp_body)

if platform.system() == 'Linux' and platform.machine() == "x86_64":
    download_url = download_options['linux_amd_64_url']
elif platform.system() == 'Linux' and platform.machine() == "aarch64":
    download_url = download_options['linux_arm_64_url']
elif platform.system() == 'Linux' and platform.machine() == "armv7l":
    download_url = download_options['linux_arm_7_url']
elif platform.system() == 'Darwin' and platform.machine() == 'arm64':
    download_url = download_options['darwin_arm_64_url']
elif platform.system() == 'Darwin' and platform.machine() == 'x86_64':
    download_url = download_options['darwin_amd_64_url']
else:
    print(f"No hishtory binary for system={platform.system()}, machine={platform.machine()}!\nIf you believe this is a mistake, please open an issue here: https://github.com/ddworken/hishtory/issues")
    sys.exit(1)

with urllib.request.urlopen(download_url) as response:
    hishtory_binary = response.read()

tmpdir = os.environ.get('TMPDIR', '') or '/tmp/'
tmpFilePath = tmpdir+'hishtory-client'
if os.path.exists(tmpFilePath):
    os.remove(tmpFilePath)
with open(tmpFilePath, 'wb') as f:
    f.write(hishtory_binary)
os.system('chmod +x ' + tmpFilePath)
exitCode = os.system(tmpFilePath + ' install')
if exitCode != 0:
    raise Exception("failed to install downloaded hishtory client via `" + tmpFilePath +" install` (is that directory mounted noexec? Consider setting an alternate directory via the TMPDIR environment variable)!")
print('Succesfully installed hishtory! Open a new terminal, try running a command, and then running `hishtory query`.')
