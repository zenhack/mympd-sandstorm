#!/bin/bash

# When you change this file, you must take manual action. Read this doc:
# - https://docs.sandstorm.io/en/latest/vagrant-spk/customizing/#setupsh

set -euo pipefail

# Download JCorporation's signing key locally and install it in a dedicated keyring
curl https://download.opensuse.org/repositories/home:/jcorporation/Debian_11/Release.key \
	| gpg --no-default-keyring --keyring /usr/share/keyrings/jcorporation.github.io.gpg --import

# ⚠️ VERIFY the fingerprint of the downloaded key (A37A ADC4 0A1C C6BE FB75  372F AA09 B8CC E895 BD7D - home:jcorporation OBS Project <home:jcorporation@build.opensuse.org>)
gpg --no-default-keyring --keyring /usr/share/keyrings/jcorporation.github.io.gpg --fingerprint

# Make the imported keyring world-readable
chmod 644 /usr/share/keyrings/jcorporation.github.io.gpg

# Get Debian VERSION_ID from os-release file
source /etc/os-release
echo $VERSION_ID

# Add JCorporation APT repository and ensure releases are signed with the repository's official keys
cat <<EOF > /etc/apt/sources.list.d/jcorporation.list
deb [signed-by=/usr/share/keyrings/jcorporation.github.io.gpg] http://download.opensuse.org/repositories/home:/jcorporation/Debian_$VERSION_ID/ ./
EOF
cat /etc/apt/sources.list.d/jcorporation.list

# Install MyMPD
apt-get update
apt-get install -y mympd

exit 0
