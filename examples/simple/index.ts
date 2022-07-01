import * as eksa from "@pulumiverse/aws-eksa";

const page = new eksa.metal.Cluster("cluster", {
  clusterName: "rawkode",
  metro: "am",
  projectId: "f4db0408-fa3d-44b4-9547-7a1f15c6d132",
  controlPlaneDeviceType: "c3.medium.x86",
});

export const adminIp = page.adminIp;
