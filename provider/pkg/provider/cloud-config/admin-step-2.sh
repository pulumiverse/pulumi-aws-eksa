#!/usr/bin/env sh
set -xe

DEBIAN_FRONTEND=noninteractive apt update && apt install --yes curl jq
curl -o /tmp/metadata.json -fsSL https://metadata.platformequinix.com/metadata
jq -r ".customdata" /tmp/metadata.json > /tmp/customdata.json

snap install go --classic
snap install yq
export HOME="/root"
systemctl restart networking
sleep 10
mkdir /tmp/eks-anywhere
cd /tmp/eks-anywhere
git clone https://github.com/aws/eks-anywhere .
make eks-a
mv bin/eksctl-anywhere /usr/local/bin
