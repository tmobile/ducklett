package azure

import (
	"fmt"
	"regexp"
)

var (
	// azure:///subscriptions/19dbcfe8-2cba-4c2d-b902-0927a4b7feec/resourceGroups/npeccpdevwu2RgGolduck/providers/Microsoft.Compute/virtualMachineScaleSets/poc-azure-w2-jhop1-pool1/virtualMachines/0
	azureProviderIDRegEx = regexp.MustCompile(`.*/subscriptions/.*/resourceGroups/.*/providers/Microsoft.Compute/virtualMachineScaleSets/(.*)/virtualMachines/(.*)`)
	// /subscriptions/19dbcfe8-2cba-4c2d-b902-0927a4b7feec/resourceGroups/npeccpdevwu2RgGolduck/providers/Microsoft.Compute/images/golduck--1.18.9--1612389336--0.3.0-osr
	azureImageReferenceRegEx = regexp.MustCompile(`/subscriptions/.*/resourceGroups/.*/providers/Microsoft.Compute/images/(.*)`)
)

// GetValuesFromProviderID takes providerID string and returns scaleset name and virtualmachine name
func GetValuesFromProviderID(p string) (string, string, error) {
	a := azureProviderIDRegEx.FindStringSubmatch(p)
	if len(a) != 3 {
		return "", "", fmt.Errorf("Failed to split providerID into proper number of pieces: %s", p)
	}
	if a[1] == "" || a[2] == "" {
		return "", "", fmt.Errorf("ProviderID had empty values for scaleset name and VM name: %s", p)
	}
	return a[1], a[2], nil
}

// GetImageIDFromImageResource takes imageResource and returns just imageID
func GetImageIDFromImageResource(p string) (string, error) {
	a := azureImageReferenceRegEx.FindStringSubmatch(p)
	if len(a) != 2 {
		return "", fmt.Errorf("Failed to split imageReference into proper number of pieces: %s", p)
	}
	if a[1] == "" {
		return "", fmt.Errorf("ImageReference had empty values for imageID: %s", p)
	}
	return a[1], nil
}
