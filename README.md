# Pulumi Component to Deploy EKSA on Equinix Metal

**Warning**: Experimental. Open an issue if you run into problems and we'll do our best to help.

## TypeScript Example

```typescript
import * as eksa from "@pulumiverse/aws-eksa";

const page = new eksa.metal.Cluster("cluster", {
  clusterName: "rawkode",
  metro: "am",
  projectId: "someProjectID",
});
```
