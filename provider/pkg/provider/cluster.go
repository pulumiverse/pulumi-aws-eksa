// Copyright 2016-2021, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package provider

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/pulumi/pulumi-cloudinit/sdk/go/cloudinit"
	"github.com/pulumi/pulumi-equinix-metal/sdk/v3/go/equinix"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi-tls/sdk/v4/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	_ "embed"
)

const ipOffset = 2

//go:embed cloud-config/admin-step-1.sh
var adminUserData1 string

//go:embed cloud-config/admin-step-2.sh
var adminUserData2 string

//go:embed cloud-config/admin-step-3.sh
var adminUserData3 string

type ClusterArgs struct {
	// Required
	ClusterName pulumi.StringInput `pulumi:"clusterName"`
	ProjectID   pulumi.StringInput `pulumi:"projectId"`
	Metro       pulumi.StringInput `pulumi:"metro"`

	// Optionals
	ControlPlaneCount      int                `pulumi:"controlPlaneCount"`
	ControlPlaneDeviceType pulumi.StringInput `pulumi:"controlPlaneDeviceType"`

	DataPlaneCount      int                `pulumi:"dataPlaneCount"`
	DataPlaneDeviceType pulumi.StringInput `pulumi:"dataPlaneDeviceType"`
}

type Cluster struct {
	pulumi.ResourceState

	AdminIp       pulumi.StringOutput `pulumi:"adminIp"`
	PrivateSshKey pulumi.StringOutput `pulumi:"privateSshKey"`
}

type AdminCustomData struct {
	ApiKey            string `json:"apiKey"`
	ClusterName       string `json:"clusterName"`
	ClusterTag        string `json:"clusterTag"`
	ProjectId         string `json:"projectID"`
	Cidr              string `json:"cidr"`
	Netmask           string `json:"netmask"`
	Gateway           string `json:"gateway"`
	AdminIp           string `json:"adminIP"`
	PoolVIP           string `json:"poolVIP"`
	TinkVIP           string `json:"tinkVIP"`
	WorkerIPs         string `json:"workerIPs"`
	PublicSshKey      string `json:"publicSshKey"`
	PrivateSshKey     string `json:"privateSshKey"`
	ControlPlaneCount int    `json:"controlPlaneCount"`
	DataPlaneCount    int    `json:"dataPlaneCount"`
}

func NewCluster(ctx *pulumi.Context,
	name string, args *ClusterArgs, opts ...pulumi.ResourceOption) (*Cluster, error) {
	if args == nil {
		args = &ClusterArgs{}
	}

	component := &Cluster{}
	err := ctx.RegisterComponentResource("aws-eksa:metal:Cluster", name, component, opts...)
	if err != nil {
		return nil, err
	}

	apiKey, err := equinix.NewProjectApiKey(ctx, "api-key", &equinix.ProjectApiKeyArgs{
		ProjectId:   args.ProjectID,
		ReadOnly:    pulumi.Bool(false),
		Description: pulumi.String(fmt.Sprintf("Read-only API key for EKSA cluster %s", args.ClusterName)),
	})

	if err != nil {
		return nil, err
	}

	clusterUniqueTag, err := random.NewRandomString(ctx, "cluster-unique-tag", &random.RandomStringArgs{
		Length:  pulumi.Int(12),
		Special: pulumi.Bool(false),
		Upper:   pulumi.Bool(false),
	})

	if err != nil {
		return nil, err
	}

	vlan, err := equinix.NewVlan(ctx, "vlan", &equinix.VlanArgs{
		Metro:     args.Metro,
		ProjectId: args.ProjectID,
	}, pulumi.Parent(component))

	if err != nil {
		return nil, err
	}

	reservedIpBlock, err := equinix.NewReservedIpBlock(ctx, "reserved-ip-block", &equinix.ReservedIpBlockArgs{
		ProjectId: args.ProjectID,
		Metro:     args.Metro,
		Type:      pulumi.String("public_ipv4"),
		Quantity:  pulumi.Int(16),
		Tags:      pulumi.StringArray{clusterUniqueTag.Result},
	}, pulumi.Parent(component))

	if err != nil {
		return nil, err
	}

	_, err = equinix.NewGateway(ctx, "gateway", &equinix.GatewayArgs{
		ProjectId:       args.ProjectID,
		VlanId:          vlan.ID(),
		IpReservationId: reservedIpBlock.ID(),
	}, pulumi.Parent(component))

	if err != nil {
		return nil, err
	}

	adminIp := reservedIpBlock.CidrNotation.ApplyT(func(cidrString string) string {
		_, network, _ := net.ParseCIDR(cidrString)
		ip, _ := cidr.Host(network, ipOffset)

		return ip.String()
	}).(pulumi.StringOutput)

	poolVip := pulumi.All(reservedIpBlock.CidrNotation, reservedIpBlock.Quantity).ApplyT(func(args []interface{}) string {
		cidrString := args[0].(string)
		quantity := args[1].(int)

		_, network, _ := net.ParseCIDR(cidrString)
		ip, _ := cidr.Host(network, quantity-2)

		return ip.String()
	}).(pulumi.StringOutput)

	tinkVip := pulumi.All(reservedIpBlock.CidrNotation, reservedIpBlock.Quantity).ApplyT(func(args []interface{}) string {
		cidrString := args[0].(string)
		quantity := args[1].(int)

		_, network, _ := net.ParseCIDR(cidrString)
		ip, _ := cidr.Host(network, quantity-3)

		return ip.String()
	}).(pulumi.StringOutput)

	workerIPs := reservedIpBlock.CidrNotation.ApplyT(func(cidrString string) string {
		totalWorkerCount := args.ControlPlaneCount + args.DataPlaneCount
		_, network, _ := net.ParseCIDR(cidrString)

		var ips []string
		for i := 0; i < totalWorkerCount; i++ {
			// +1 for the adminIp above, pool and tink vip use quantity - 3 so don't need skipped
			ip, _ := cidr.Host(network, ipOffset+1+i)
			ips = append(ips, ip.String())
		}

		return strings.Join(ips, ",")
	}).(pulumi.StringOutput)

	for i := 1; i <= args.ControlPlaneCount; i++ {
		device, err := equinix.NewDevice(ctx, fmt.Sprintf("control-plane-%d", i), &equinix.DeviceArgs{
			Hostname:        pulumi.String(fmt.Sprintf("control-plane-%d", i)),
			Plan:            args.ControlPlaneDeviceType,
			Metro:           args.Metro,
			ProjectId:       args.ProjectID,
			OperatingSystem: pulumi.String("custom_ipxe"),
			IpxeScriptUrl: adminIp.ApplyT(func(adminIp string) string {
				return fmt.Sprintf("http://%s/ipxe/", adminIp)
			}).(pulumi.StringOutput),
			AlwaysPxe:    pulumi.Bool(true),
			BillingCycle: pulumi.String("hourly"),
			Tags:         pulumi.StringArray{pulumi.String("control-plane"), pulumi.String("tink-worker"), clusterUniqueTag.Result},
		})

		if err != nil {
			return nil, err
		}

		networkType, err := equinix.NewDeviceNetworkType(ctx, fmt.Sprintf("control-plane-%d", i), &equinix.DeviceNetworkTypeArgs{
			DeviceId: device.ID(),
			Type:     pulumi.String("layer2-individual"),
		})

		if err != nil {
			return nil, err
		}

		_, err = equinix.NewPortVlanAttachment(ctx, fmt.Sprintf("control-plane-%d", i), &equinix.PortVlanAttachmentArgs{
			DeviceId: device.ID(),
			VlanVnid: vlan.Vxlan,
			PortName: pulumi.String("eth0"),
		}, pulumi.DependsOn([]pulumi.Resource{networkType}))

		if err != nil {
			return nil, err
		}
	}

	for i := 1; i <= args.DataPlaneCount; i++ {
		device, err := equinix.NewDevice(ctx, fmt.Sprintf("data-plane-%d", i), &equinix.DeviceArgs{
			Hostname:        pulumi.String(fmt.Sprintf("data-plane-%d", i)),
			Plan:            args.DataPlaneDeviceType,
			Metro:           args.Metro,
			ProjectId:       args.ProjectID,
			OperatingSystem: pulumi.String("custom_ipxe"),
			IpxeScriptUrl: reservedIpBlock.CidrNotation.ApplyT(func(cidrString string) string {
				_, network, _ := net.ParseCIDR(cidrString)
				ip, _ := cidr.Host(network, 2)

				return fmt.Sprintf("http://%s/ipxe/", ip.String())
			}).(pulumi.StringOutput),
			AlwaysPxe:    pulumi.Bool(true),
			BillingCycle: pulumi.String("hourly"),
			Tags:         pulumi.StringArray{pulumi.String("data-plane"), pulumi.String("tink-worker"), clusterUniqueTag.Result},
		})

		if err != nil {
			return nil, err
		}

		networkType, err := equinix.NewDeviceNetworkType(ctx, fmt.Sprintf("data-plane-%d", i), &equinix.DeviceNetworkTypeArgs{
			DeviceId: device.ID(),
			Type:     pulumi.String("layer2-individual"),
		})

		if err != nil {
			return nil, err
		}

		_, err = equinix.NewPortVlanAttachment(ctx, fmt.Sprintf("data-plane-%d", i), &equinix.PortVlanAttachmentArgs{
			DeviceId: device.ID(),
			VlanVnid: vlan.Vxlan,
			PortName: pulumi.String("eth0"),
		}, pulumi.DependsOn([]pulumi.Resource{networkType}))

		if err != nil {
			return nil, err
		}
	}

	privateKey, err := tls.NewPrivateKey(ctx, "private-key", &tls.PrivateKeyArgs{
		Algorithm: pulumi.String("RSA"),
		RsaBits:   pulumi.Int(4096),
	})

	if err != nil {
		return nil, err
	}

	randomKeySuffix, err := random.NewRandomString(ctx, "ssh-key-suffix", &random.RandomStringArgs{
		Length:  pulumi.Int(3),
		Special: pulumi.Bool(false),
		Upper:   pulumi.Bool(false),
	})

	if err != nil {
		return nil, err
	}

	_, err = equinix.NewSshKey(ctx, "ssh-key", &equinix.SshKeyArgs{
		Name: pulumi.All(args.ClusterName, randomKeySuffix.Result).ApplyT(func(args []interface{}) string {
			clusterName := args[0].(string)
			suffix := args[1].(string)

			return fmt.Sprintf("%s-%s", clusterName, suffix)
		}).(pulumi.StringOutput),
		PublicKey: privateKey.PublicKeyOpenssh,
	})

	if err != nil {
		return nil, err
	}

	// Admin Cloud Config
	config, err := cloudinit.NewConfig(ctx, "admin", &cloudinit.ConfigArgs{
		Gzip:         pulumi.Bool(false),
		Base64Encode: pulumi.Bool(false),
		Parts: &cloudinit.ConfigPartArray{
			&cloudinit.ConfigPartArgs{
				ContentType: pulumi.String("text/cloud-config"),
				Content:     pulumi.String(adminUserData1),
			},
			&cloudinit.ConfigPartArgs{
				ContentType: pulumi.String("text/x-shellscript"),
				Content:     pulumi.String(adminUserData2),
			},
			&cloudinit.ConfigPartArgs{
				ContentType: pulumi.String("text/x-shellscript"),
				Content:     pulumi.String(adminUserData3),
			},
		},
	})

	if err != nil {
		return nil, err
	}

	device, err := equinix.NewDevice(ctx, "admin", &equinix.DeviceArgs{
		Hostname:        pulumi.String("admin"),
		Plan:            args.ControlPlaneDeviceType,
		Metro:           args.Metro,
		ProjectId:       args.ProjectID,
		OperatingSystem: pulumi.String("ubuntu_20_04"),
		BillingCycle:    pulumi.String("hourly"),
		Tags:            pulumi.StringArray{pulumi.String("tink-provisioner")},
		UserData:        config.Rendered,
		CustomData: pulumi.All(apiKey.Token, clusterUniqueTag.Result, args.ProjectID, reservedIpBlock.CidrNotation, reservedIpBlock.Netmask, reservedIpBlock.Gateway, adminIp, poolVip, tinkVip, workerIPs, privateKey.PublicKeyOpenssh, privateKey.PrivateKeyOpenssh, args.ClusterName).ApplyT(func(vars []interface{}) (string, error) {
			token := vars[0].(string)
			clusterTag := vars[1].(string)
			projectID := vars[2].(string)
			cidr := vars[3].(string)
			netmask := vars[4].(string)
			gateway := vars[5].(string)
			adminIp := vars[6].(string)
			poolVip := vars[7].(string)
			tinkVip := vars[8].(string)
			workerIps := vars[9].(string)
			publicSshKey := vars[10].(string)
			privateSshKey := vars[11].(string)
			clusterName := vars[12].(string)

			json, err := json.Marshal(&AdminCustomData{
				ApiKey:            token,
				ClusterName:       clusterName,
				ClusterTag:        clusterTag,
				ProjectId:         projectID,
				Cidr:              cidr,
				Netmask:           netmask,
				Gateway:           gateway,
				AdminIp:           adminIp,
				PoolVIP:           poolVip,
				TinkVIP:           tinkVip,
				WorkerIPs:         workerIps,
				PublicSshKey:      publicSshKey,
				PrivateSshKey:     privateSshKey,
				ControlPlaneCount: args.ControlPlaneCount,
				DataPlaneCount:    args.DataPlaneCount,
			})

			if err != nil {
				return "", err
			}

			return string(json), nil
		}).(pulumi.StringOutput),
	})

	if err != nil {
		return nil, err
	}

	networkType, err := equinix.NewDeviceNetworkType(ctx, "admin", &equinix.DeviceNetworkTypeArgs{
		DeviceId: device.ID(),
		Type:     pulumi.String("hybrid"),
	})

	if err != nil {
		return nil, err
	}

	_, err = equinix.NewPortVlanAttachment(ctx, "admin", &equinix.PortVlanAttachmentArgs{
		DeviceId: device.ID(),
		VlanVnid: vlan.Vxlan,
		PortName: pulumi.String("bond0"),
	}, pulumi.DependsOn([]pulumi.Resource{networkType}))

	if err != nil {
		return nil, err
	}

	component.AdminIp = adminIp
	component.PrivateSshKey = privateKey.PrivateKeyOpenssh

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"adminIp":       adminIp,
		"privateSshKey": privateKey.PrivateKeyOpenssh,
	}); err != nil {
		return nil, err
	}

	return component, nil
}
