#!/bin/bash
set -euo pipefail

mkdir -p /var/{cache,lib}/mympd

mympd &
/opt/app/mympd-proxy
exit 0
