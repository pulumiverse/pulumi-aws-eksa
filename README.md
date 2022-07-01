# Pulumi Component for Amazon EKS Anywhere Bare Metal 
This component demonstrates how to stand up EKS Anywhere Bare Metal on Equinix Metal.

## Background
[EKS Anywhere Bare Metal](https://aws.amazon.com/eks/eks-anywhere/) removes prior dependencies on VMware vSphere from EKS Anywhere, simpllfying the process of using EKS Anywhere in your own datacenter.

This component demonstrates how to leverage EKS Anywhere on [Equinix Metal](https://metal.equinix.com/) - a provider of bare metal hosting services.



## General process for using EKS Anywhere Bare Metal

1. Configure Equinix Metal instances using Pulumi
2. Use  eks-a generate to configure hardware
3. Use eks-a generate to configure your cluster
4. Create a bootstrap cluster on your admin machine
5. Provision and create control plane bare metal servers
6. Provision and create bare metal worker nodes
7. Move cluster management to the workload cluster
8. Delete the bootstrap cluster


## Prerequisites

This example requires both AWS and Equinix Metal accounts. 
