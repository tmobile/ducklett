/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-01/compute"
	klog "k8s.io/klog/v2"
)

// GetImageReferenceFromVMSS returns imageReference from what's currently in VMSS
func GetImageReferenceFromVMSS(ctx context.Context, client AzureVirtualMachineScaleSetsClientAPI, rg, ssName string) (string, error) {
	// Use VMSS client to find imageResource for VM in VMSS
	vmss, err := client.Get(ctx, rg, ssName)
	if err != nil {
		return "", err
	}
	if vmss.VirtualMachineProfile.StorageProfile.ImageReference.ID == nil {
		return "", fmt.Errorf("ImageReference.ID was nil while getting from VMSS")
	}
	return *vmss.VirtualMachineProfile.StorageProfile.ImageReference.ID, nil
}

// IsVMSSUpdating checks if VMSS is in updating state
func IsVMSSUpdating(ctx context.Context, client AzureVirtualMachineScaleSetsClientAPI, rg, ssName string) (bool, error) {
	// Use VMSS client get VMSS and check status
	vmss, err := client.Get(ctx, rg, ssName)
	if err != nil {
		return false, err
	}
	if vmss.VirtualMachineProfile.StorageProfile.ImageReference.ID == nil {
		return false, fmt.Errorf("ImageReference.ID was nil while getting from VMSS")
	}
	// VMSS seems to have just string pointer to string for status, no hardcoded type :(
	if vmss.ProvisioningState != nil && *vmss.ProvisioningState == "Updating" {
		return true, nil
	}
	return false, nil
}

// DeleteVMFromVMSS deletes the specified VM from the VMSS
func DeleteVMFromVMSS(ctx context.Context, client AzureVirtualMachineScaleSetsClientAPI, rg, ssName, vmName string) error {
	// Attempt to delete VM from VMSS
	vms := []string{vmName}
	vmIDs := compute.VirtualMachineScaleSetVMInstanceIDs{
		InstanceIds: &vms,
	}
	future, err := client.DeleteInstances(ctx, rg, ssName, compute.VirtualMachineScaleSetVMInstanceRequiredIDs(vmIDs))
	if err != nil {
		return fmt.Errorf("failed to delete instance for VM named %s in scaleset %s: %v", vmName, ssName, err)
	}
	// Start async process to watch for completion of instance delete
	go func() {
		klog.V(3).Infof("Waiting for completion of instance delete for VM named %s in scaleset %s", vmName, ssName)
		err = future.WaitForCompletionRef(ctx, client.GetClient())
		if err != nil {
			klog.Errorf("DeleteVMFromVSS failed for VM named %s in scaleset %s with error: %v", vmName, ssName, err)
		} else {
			klog.V(3).Infof("DeleteVMFromVSS deleted instance for VM named %s in scaleset %s", vmName, ssName)
		}
	}()
	return nil
}

// ScaleUpVMSS attempts to increase scaleset, increasing by num
func ScaleUpVMSS(ctx context.Context, client AzureVirtualMachineScaleSetsClientAPI, rg, ssName string, num int64) error {
	// Get current capacity of VMSS
	vmss, err := client.Get(ctx, rg, ssName)
	if err != nil {
		return fmt.Errorf("error getting VMSS to scale up: %v", err)
	}
	if vmss.Sku == nil || vmss.Sku.Capacity == nil {
		return fmt.Errorf("vmss sku or sku.capacity was nil?")
	}
	newCapacity := *vmss.Sku.Capacity + num

	// Check max limit and error if this would push it over
	if v, ok := vmss.Tags["max"]; ok {
		max, err := strconv.Atoi(*v)
		if err != nil {
			return fmt.Errorf("could not convert Tags[max] %v to int: %v", v, err)
		}
		if newCapacity > int64(max) {
			return fmt.Errorf("cannot increase VMSS, max is %v and attempting to increase by %v to %v", v, num, newCapacity)
		}
	}
	// Set up VMSS with new capacity
	sku := compute.Sku{
		Capacity: &newCapacity,
	}
	vmSSUpdate := compute.VirtualMachineScaleSetUpdate{
		Sku: &sku,
	}
	klog.Infof("ScaleUpVMSS attempting to scale up VMSS %v by %v to %v", ssName, num, newCapacity)
	future, err := client.Update(ctx, rg, ssName, vmSSUpdate)
	if err != nil {
		return fmt.Errorf("failed to scale up VMSS: %v", err)
	}
	// Start async process to watch for completion of update
	go func() {
		klog.V(3).Infof("Waiting for completion of ScaleUpVMSS for %s", ssName)
		err = future.WaitForCompletionRef(ctx, client.GetClient())
		if err != nil {
			klog.Errorf("ScaleUpVMSS failed to scale to %d for %s with error: %v", newCapacity, ssName, err)
		} else {
			klog.Infof("ScaleUpVMSS updated to new capacity %d for %s", newCapacity, ssName)
		}
	}()
	return nil
}

// GetInstancesInVMSS returns list of instances in a VMSS, including provisioning state
func GetInstancesInVMSS(ctx context.Context, client VirtualMachineScaleSetVMsClientAPI, rg, ssName string) (map[string]map[string]string, error) {
	vmList, err := client.ListComplete(ctx, rg, ssName, "", "", "")
	if err != nil {
		return nil, err
	}
	instances := make(map[string]map[string]string)
	for _, vm := range *vmList.Response().Value {
		klog.Infof("Read instance %s (%s) in state %s", *vm.Name, *vm.OsProfile.ComputerName, *vm.VirtualMachineScaleSetVMProperties.ProvisioningState)
		instance := make(map[string]string)
		instance["id"] = *vm.ID
		instance["instanceID"] = *vm.InstanceID
		instance["state"] = *vm.VirtualMachineScaleSetVMProperties.ProvisioningState

		// Convert VM name to lowercase
		instances[strings.ToLower(*vm.OsProfile.ComputerName)] = instance
	}
	return instances, nil
}

// GetVMNameInVMSS returns the VM name of the machine in a VMSS
func GetVMNameInVMSS(ctx context.Context, client VirtualMachineScaleSetVMsClientAPI, rg, ssName, nodeName string) (string, error) {
	vmList, err := client.ListComplete(ctx, rg, ssName, "", "", "")
	if err != nil {
		return "", err
	}
	for _, vm := range *vmList.Response().Value {
		// DANGER: instances in VMSS can have uppercase letters (like sbx-85fa76-cp00000A), but they are converted to lowercase
		// in the OS for hostname and also node name, so we must compare these accordingly below
		if strings.ToLower(*vm.OsProfile.ComputerName) == nodeName {
			return *vm.InstanceID, nil
		}
	}
	return "", fmt.Errorf("failed to find VM for node %s in VMSS %s", nodeName, ssName)
}

// RestartVMInVMSS restarts the VM in the scaleset, normally to handle Failed states
func RestartVMInVMSS(ctx context.Context, client VirtualMachineScaleSetVMsClientAPI, rg, ssName, InstanceID string) error {
	future, err := client.Restart(ctx, rg, ssName, InstanceID)
	if err != nil {
		return fmt.Errorf("failed to restart VM %s in VMSS %s: %v", InstanceID, ssName, err)
	}
	// Start async process to watch for completion of restart
	go func() {
		klog.V(3).Infof("Waiting for completion of RestartVMInVMSS for VM %s in VMSS %s", InstanceID, ssName)
		err = future.WaitForCompletionRef(ctx, client.GetClient())
		if err != nil {
			klog.Errorf("RestartVMInVMSS failed to restart VM %s in VMSS %s: %v", InstanceID, ssName, err)
		} else {
			klog.Infof("RestartVMInVMSS restarated VM %s in VMSS %s", InstanceID, ssName)
		}
	}()
	return nil
}
