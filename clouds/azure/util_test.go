package azure

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// GetValuesFromProviderID(p string) (string, string, error)
// GetImageIDFromImageResource(p string) (string, error)

var (
	testProviderID  = "azure:///subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx/resourceGroups/xxxxxxxxx/providers/Microsoft.Compute/virtualMachineScaleSets/testscalesetname/virtualMachines/vm0"
	testResourceURL = "/subscriptions/xxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx/resourceGroups/xxxxxxxxx/providers/Microsoft.Compute/images/conducktor-flatcar-1.20.8-v0.3.0-a424b18d"
)

func TestGetValuesFromProviderID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		in := testProviderID
		got1, got2, got3 := GetValuesFromProviderID(in)
		require.Equal(t, "testscalesetname", got1)
		require.Equal(t, "vm0", got2)
		require.Equal(t, nil, got3)
	})
}

func TestGetImageIDFromImageResource(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		in := testResourceURL
		got1, got2 := GetImageIDFromImageResource(in)
		require.Equal(t, "conducktor-flatcar-1.20.8-v0.3.0-a424b18d", got1)
		require.Equal(t, nil, got2)
	})
}
