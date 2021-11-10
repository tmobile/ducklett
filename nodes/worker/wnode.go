package worker

import (
	"context"
	"fmt"

	"github.com/tmobile/ducklett/clouds/azure"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	workerNodeLabel = "nodegroup in (agent,telemetry,storage)"
)

// NodeHandler holds context and kubernetes client
type NodeHandler struct {
	Ctx               context.Context
	SubscriptionID    string
	ResourceGroup     string
	KubeClientSet     kubernetes.Interface
	AzureVMSSClient   azure.AzureVirtualMachineScaleSetsClientAPI
	AzureVMSSVMClient azure.VirtualMachineScaleSetVMsClientAPI
	ClusterName       string
	ConcurrencyMax    int
}

// NewNodeHandler returns new Node object for worker node interface
func NewNodeHandler(ctx context.Context, clusterName, subID, rg string, client kubernetes.Interface, con int) *NodeHandler {
	return &NodeHandler{
		Ctx:            ctx,
		SubscriptionID: subID,
		ResourceGroup:  rg,
		KubeClientSet:  client,
		ClusterName:    clusterName,
		ConcurrencyMax: con,
	}
}

// GetNodeTypeName returns new Node object for worker node interface
func (n *NodeHandler) GetNodeTypeName() string {
	return "worker"
}

// GetConcurrencyMax returns concurrency max
func (n *NodeHandler) GetConcurrencyMax() int {
	return n.ConcurrencyMax
}

// GetNodeList returns list of worker nodes
func (n *NodeHandler) GetNodeList() (*apiv1.NodeList, error) {
	return n.KubeClientSet.CoreV1().Nodes().List(n.Ctx, metav1.ListOptions{
		LabelSelector: workerNodeLabel,
	})
}

// GetPodsToIgnore returns array of pod types to ignore in a drain
func (n *NodeHandler) GetPodsToIgnore() []string {
	return []string{"DaemonSet"}
}

// GetSSName attempts to get the VMSS name the node is in
func (n *NodeHandler) GetSSName(node apiv1.Node) (string, error) {
	if node.Spec.ProviderID == "" {
		return "", fmt.Errorf("node has no providerID")
	}
	ssName, _, err := azure.GetValuesFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return "", fmt.Errorf("failed to get values from providerID for node %s: %v", node.GetName(), err)
	}
	return ssName, nil
}

// PrepForDelete does nothing for worker nodes yet
func (n *NodeHandler) PrepForDelete(node apiv1.Node, imageName string) error {
	// Placeholder for now
	return nil
}

// GetVMName gets VM name in VMSS
func (n *NodeHandler) GetVMName(ssName string, node apiv1.Node, client azure.VirtualMachineScaleSetVMsClientAPI) (string, error) {
	// Get scaleset and VM name for this node
	_, vmName, err := azure.GetValuesFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return "", fmt.Errorf("failed to get values from providerID for node %s: %v", node.GetName(), err)
	}
	return vmName, nil
}
