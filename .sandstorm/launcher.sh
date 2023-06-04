#!/bin/bash
set -euo pipefail

mkdir -p /var/{cache,lib}/mympd

mympd
exit 0
