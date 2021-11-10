package azure

import (
	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-01/compute/computeapi"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
)

var _ AzureVirtualMachineScaleSetsClientAPI = (*AzureVirtualMachineScaleSetsClient)(nil)

// AzureVirtualMachineScaleSetsClientAPI mock of Azure VMSS client
type AzureVirtualMachineScaleSetsClientAPI interface {
	computeapi.VirtualMachineScaleSetsClientAPI
	GetClient() autorest.Client
}

type AzureVirtualMachineScaleSetsClient struct {
	compute.VirtualMachineScaleSetsClient
}

func (c AzureVirtualMachineScaleSetsClient) GetClient() autorest.Client {
	return c.Client
}

// GetVMSSClient returns a client for Azure VMSS resources
func GetVMSSClient(subID string) (AzureVirtualMachineScaleSetsClientAPI, error) {
	client := AzureVirtualMachineScaleSetsClient{
		VirtualMachineScaleSetsClient: compute.NewVirtualMachineScaleSetsClient(subID),
	}

	authorizer, err := auth.NewAuthorizerFromEnvironment()
	if err != nil {
		return nil, err
	}
	client.Authorizer = authorizer

	return client, nil
}

var _ VirtualMachineScaleSetVMsClientAPI = (*VirtualMachineScaleSetVMsClient)(nil)

type VirtualMachineScaleSetVMsClientAPI interface {
	computeapi.VirtualMachineScaleSetVMsClientAPI
	GetClient() autorest.Client
}

type VirtualMachineScaleSetVMsClient struct {
	compute.VirtualMachineScaleSetVMsClient
}

func (c VirtualMachineScaleSetVMsClient) GetClient() autorest.Client {
	return c.Client
}

// GetVMClient returns a client for Azure VMs
func GetVMSSVMClient(subID string) (VirtualMachineScaleSetVMsClientAPI, error) {
	client := VirtualMachineScaleSetVMsClient{
		VirtualMachineScaleSetVMsClient: compute.NewVirtualMachineScaleSetVMsClient(subID),
	}

	authorizer, err := auth.NewAuthorizerFromEnvironment()
	if err != nil {
		return nil, err
	}
	client.Authorizer = authorizer

	return client, nil
}
