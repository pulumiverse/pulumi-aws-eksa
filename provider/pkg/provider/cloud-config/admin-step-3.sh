#!/usr/bin/env /usr/bin/bash
set -xe

export HOME="/root"

YQ_VERSION="4.25.3"
YQ_BINARY="yq_linux_amd64"
curl -fsSL -o /usr/local/bin/yq https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/${YQ_BINARY}
chmod +x /usr/local/bin/yq

/snap/bin/go install github.com/equinix/metal-cli/cmd/metal@latest

export METAL_AUTH_TOKEN=$(jq -r ".apiKey" /tmp/customdata.json)

CLUSTER_NAME=$(jq -r ".clusterName" /tmp/customdata.json)
CLUSTER_TAG=$(jq -r ".clusterTag" /tmp/customdata.json)
PROJECT_ID=$(jq -r ".projectID" /tmp/customdata.json)
CIDR=$(jq -r ".cidr" /tmp/customdata.json)
NETMASK=$(jq -r ".netmask" /tmp/customdata.json)
GATEWAY=$(jq -r ".gateway" /tmp/customdata.json)
ADMIN_IP=$(jq -r ".adminIP" /tmp/customdata.json)
POOL_VIP=$(jq -r ".poolVIP" /tmp/customdata.json)
TINK_VIP=$(jq -r ".tinkVIP" /tmp/customdata.json)
WORKER_IPS=$(jq -r ".workerIPs" /tmp/customdata.json)
PUBLIC_SSH_KEY=$(jq -r ".publicSshKey" /tmp/customdata.json)
PRIVATE_SSH_KEY=$(jq -r ".privateSshKey" /tmp/customdata.json)
CONTROL_PLANE_COUNT=$(jq -r ".controlPlaneCount" /tmp/customdata.json)
DATA_PLANE_COUNT=$(jq -r ".dataPlaneCount" /tmp/customdata.json)

# Get array of worker IPs
IFS=',' read -ra worker_ips <<< "${WORKER_IPS}"

HARDWARE=/root/hardware.csv

touch ${HARDWARE}
echo "hostname,vendor,mac,ip_address,gateway,netmask,nameservers,disk,labels" >> ${HARDWARE}

IP_INDEX=0

while IFS="\n" read -r id; do
    # Reboot
    /root/go/bin/metal device reboot --id ${id}

    # Add to hardware file
    HOSTNAME=$(/root/go/bin/metal device list -p ${PROJECT_ID} -o json | jq -r ". | map(select(.id == \"${id}\")) | .[0].hostname")
    TYPE=$(/root/go/bin/metal device list -p ${PROJECT_ID} -o json     | jq -r ". | map(select(.id == \"${id}\")) | .[0].hostname | if startswith(\"control\") then \"cp\" else \"dp\" end")
    MAC=$(/root/go/bin/metal device list -p ${PROJECT_ID} -o json      | jq -r ". | map(select(.id == \"${id}\")) | {network_ports: .[].network_ports} | select(.network_ports[].name==\"eth0\") | .network_ports | map(select(.name == \"eth0\")) | .[0].data.mac")

    echo "${HOSTNAME},Equinix,${MAC},${worker_ips[IP_INDEX]},${GATEWAY},${NETMASK},8.8.8.8,/dev/sda,type=${TYPE}" >> ${HARDWARE}

    IP_INDEX=$((IP_INDEX+1))
done< <(/root/go/bin/metal device list -p ${PROJECT_ID} -o json | jq -r ". | map(select(.tags[] | contains(\"${CLUSTER_TAG}\"))) | .[].id")


mkdir -p ${HOME}/.ssh
chmod 700 ${HOME}/.ssh

cat <<EOF > ${HOME}/.ssh/key
${PRIVATE_SSH_KEY}
EOF
chmod 600 ${HOME}/.ssh/key

cat <<EOF > ${HOME}/.ssh/key.pub
${PUBLIC_SSH_KEY}
EOF
chmod 644 ${HOME}/.ssh/key.pub

export TINKERBELL_HOST_IP=${TINK_VIP}
export CLUSTER_NAME=${CLUSTER_NAME}
export TINKERBELL_PROVIDER=true
export CONTROL_PLANE_VIP=${POOL_VIP}
export CLUSTER_CONFIG_FILE=${CLUSTER_NAME}.yaml
export PUB_SSH_KEY=\"$(cat /root/.ssh/key.pub)\"

eksctl-anywhere generate clusterconfig ${CLUSTER_NAME} --provider tinkerbell > ${CLUSTER_CONFIG_FILE}
cp ${CLUSTER_CONFIG_FILE} ${CLUSTER_CONFIG_FILE}.orig

/usr/local/bin/yq e -i "select(.kind == \"Cluster\").spec.controlPlaneConfiguration.endpoint.host |= \"${POOL_VIP}\"" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.kind == \"Cluster\").spec.controlPlaneConfiguration.count |= ${CONTROL_PLANE_COUNT}" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.spec.workerNodeGroupConfigurations[].machineGroupRef.kind == \"TinkerbellMachineConfig\").spec.workerNodeGroupConfigurations[0].count |= ${DATA_PLANE_COUNT}" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.kind == \"TinkerbellDatacenterConfig\").spec.tinkerbellIP |= \"${TINK_VIP}\"" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.kind == \"TinkerbellMachineConfig\").spec.users[].sshAuthorizedKeys[0] |= \"${PUBLIC_SSH_KEY}\"" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.kind == \"TinkerbellMachineConfig\").spec.osFamily |= \"ubuntu\"" ${CLUSTER_CONFIG_FILE}
/usr/local/bin/yq e -i "select(.kind == \"TinkerbellMachineConfig\").spec.hardwareSelector |= { \"type\": \"HW_TYPE\" }" ${CLUSTER_CONFIG_FILE}

sed -i '0,/HW_TYPE/s//cp/' ${CLUSTER_CONFIG_FILE}
sed -i '0,/HW_TYPE/s//dp/' ${CLUSTER_CONFIG_FILE}

eksctl-anywhere -v9 create cluster --filename ${CLUSTER_CONFIG_FILE} --hardware-csv ${HARDWARE} --tinkerbell-bootstrap-ip ${ADMIN_IP} 2>&1 > /root/eksa-create-cluster.log

echo "To follow along, tail -f /root/eksa-create-cluster.log"
