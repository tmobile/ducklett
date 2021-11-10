package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tmobile/ducklett/clouds/azure"
)

var (
	imageID = flag.String("imageid", "", "New imageID tag")
	ctx     = context.TODO()
)

func main() {
	flag.Parse()

	if *imageID == "" {
		bail("Missing argument", "imageID")
	}
	subID := os.Getenv("ARM_SUBSCRIPTION_ID")
	if subID == "" {
		bail("Missing env", "ARM_SUBSCRIPTION_ID")
	}
	rGroup := os.Getenv("ARM_RESOURCE_GROUP")
	if rGroup == "" {
		bail("Missing env", "ARM_RESOURCE_GROUP")
	}
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		bail("Missing env", "CLUSTER_NAME")
	}

	// Update imageID of  VMSSes
	vmssClient, err := azure.GetVMSSClient(subID)
	if err != nil {
		bail("Failed to get VMSS client", err.Error())
	}
	// TODO: may need to make this more dynamic in the future
	vmssNames := []string{"cp", "agent-pool", "storage-pool", "telemetry-pool"}
	for _, vmssName := range vmssNames {
		ssName := fmt.Sprintf("%s-%s", clusterName, vmssName)
		fmt.Println("Attmpting to get VMSS", ssName)
		vmss, err := vmssClient.Get(ctx, rGroup, ssName)
		if err != nil {
			bail("Failed to get VMSS", err.Error())
		}
		fmt.Printf("VMSS %s currently has imageID %s\n", ssName, *vmss.VirtualMachineProfile.StorageProfile.ImageReference.ID)

		imageIDURL := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/images/%s", subID, rGroup, *imageID)
		*vmss.VirtualMachineProfile.StorageProfile.ImageReference.ID = imageIDURL

		fmt.Printf("Attmpting to update image of VMSS %s to %s...\n", ssName, imageIDURL)

		future, err := vmssClient.CreateOrUpdate(ctx, rGroup, ssName, vmss)
		if err != nil {
			bail("Failed to update VMSS", err.Error())
		}
		fmt.Println("Waiting for operation to complete")
		err = future.WaitForCompletionRef(ctx, vmssClient.GetClient())
		if err != nil {
			bail("Failed while waiting for completion of update to VMSS", err.Error())
		}
	}
}

func bail(m, err string) {
	fmt.Printf("%s: %s", m, err)
	os.Exit(1)
}
