#cloud-config
apt:
  sources: 
    docker.list:
      source: deb [arch=amd64] https://download.docker.com/linux/ubuntu $RELEASE stable
      keyid: 9DC858229FC7DD38854AE2D88D81803C0EBFCD88
    kubernetes.list:
      source: deb [arch=amd64] https://apt.kubernetes.io/ kubernetes-xenial main
      keyid: 7F92E05B31093BEF5A3C2D38FEEA9169307EA071
    kubernetes-key-2:
      keyid: 59FE0256827269DC81578F928B57C5C2836F4BEB 

packages:
- apt-transport-https
- ca-certificates
- curl
- make
- gnupg
- lsb-release
- kubectl
- jq
- gnupg-agent
- gnupg2
- software-properties-common
- containerd.io
- docker-ce-cli
- docker-ce

write_files:
- content: |
    auto bond0.${vlan_vnid}
      iface bond0.${vlan_vnid} inet static
      pre-up sleep 5
      address ${admin_ip}
      netmask ${netmask}
      vlan-raw-device bond0
  append: true
  path: /etc/network/interfaces
