#!/usr/bin/env sh
set -xe

DEBIAN_FRONTEND=noninteractive apt update && apt install --yes curl jq
curl -o /tmp/metadata.json -fsSL https://metadata.platformequinix.com/metadata
jq -r ".customdata" /tmp/metadata.json > /tmp/customdata.json
