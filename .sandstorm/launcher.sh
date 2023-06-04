#!/bin/bash
set -euo pipefail

mkdir -p /var/{cache,lib}/mympd
mkdir -p /var/ipnetwork-proxy

mympd &
/opt/app/mympd-proxy
exit 0
