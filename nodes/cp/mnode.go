package master

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/tmobile/ducklett/clouds/azure"

	etcdv3 "go.etcd.io/etcd/client/v3"
	apiv1 "k8s.io/api/core/v1"

	//"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	kubeadmv1 "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta2"
	"sigs.k8s.io/yaml"
)

const (
	masterNodeLabel     = "node-role.kubernetes.io/master"
	cpVMSSNamingPattern = "%s-cp"
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

// NewNodeHandler returns new Node object for master node interface
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
	return "controlplane"
}

// GetNodeList returns list of master nodes
func (n *NodeHandler) GetNodeList() (*apiv1.NodeList, error) {
	return n.KubeClientSet.CoreV1().Nodes().List(n.Ctx, metav1.ListOptions{
		LabelSelector: masterNodeLabel,
	})
}

// GetConcurrencyMax returns concurrency max
func (n *NodeHandler) GetConcurrencyMax() int {
	return n.ConcurrencyMax
}

// GetPodsToIgnore returns array of pod types to ignore in a drain
func (n *NodeHandler) GetPodsToIgnore() []string {
	return []string{"DaemonSet", "Node"}
}

// GetSSName attempts to get the VMSS name the node is in
func (n *NodeHandler) GetSSName(node apiv1.Node) (string, error) {
	// Use static naming convention for cp VMSS and check the image reference on it
	return fmt.Sprintf(cpVMSSNamingPattern, n.ClusterName), nil
}

// PrepForDelete prepares node for deletion by deleting it from etcd (LB will be handled when node is deleted from VMSS)
func (n *NodeHandler) PrepForDelete(node apiv1.Node, imageName string) error {
	// Update kubeadm-config configMap and remove this node from apiEndpoints
	klog.V(3).Infof("Preparing to updateKubeadmConfigMapRemoveNode for node %s", node.GetName())
	err := n.updateKubeadmConfigMapRemoveNode(node.GetName())
	if err != nil {
		return err
	}
	// Delete from etcd
	endpoints, err := n.getEtcdEndpoints()
	if err != nil {
		return err
	}
	klog.V(3).Infof("Preparing to deleteEtcdMember for node %s with endpoints %s", node.GetName(), strings.Join(endpoints, ", "))
	return deleteEtcdMember(getIPofNode(node.Status.Addresses), node.GetName(), endpoints)
}

// updateKubeadmConfigMapRemoveNode removes nodeName from apiEndpoints
func (n *NodeHandler) updateKubeadmConfigMapRemoveNode(nodeName string) error {
	cm, err := n.KubeClientSet.CoreV1().ConfigMaps("kube-system").Get(n.Ctx, "kubeadm-config", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if statusYAML, ok := cm.Data["ClusterStatus"]; ok {
		clusterStatus := kubeadmv1.ClusterStatus{}
		err := yaml.Unmarshal([]byte(statusYAML), &clusterStatus)
		if err != nil {
			return fmt.Errorf("failed to decode ClusterStatus in data of kubeadm-config configMap: %v", err)
		}
		if _, ok := clusterStatus.APIEndpoints[nodeName]; ok {
			delete(clusterStatus.APIEndpoints, nodeName)
			statusBytes, err := yaml.Marshal(clusterStatus)
			if err != nil {
				return fmt.Errorf("failed to marshal ClusterStatus back to yaml: %v", err)
			}
			cm.Data["ClusterStatus"] = string(statusBytes)
		} else {
			klog.V(3).Infof("Failed to find nodeName %s in apiEndpoints, continuing\n", nodeName)
		}
	} else {
		return fmt.Errorf("failed to find ClusterStatus in data of kubeadm-config configMap")
	}
	// Write the updated configMap back to the cluster
	_, err = n.KubeClientSet.CoreV1().ConfigMaps("kube-system").Update(n.Ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// GetVMName gets VM name in VMSS
func (n *NodeHandler) GetVMName(ssName string, node apiv1.Node, client azure.VirtualMachineScaleSetVMsClientAPI) (string, error) {
	// Get VM name for this node
	vmName, err := azure.GetVMNameInVMSS(n.Ctx, client, n.ResourceGroup, ssName, node.GetName())
	if err != nil {
		return "", err
	}
	return vmName, nil
}

func getIPofNode(addys []apiv1.NodeAddress) string {
	// Sift through addresses for node, find internal IP
	for _, addy := range addys {
		if addy.Type == apiv1.NodeInternalIP {
			return addy.Address
		}
	}
	// This should never ever fail...right?
	return ""
}

func (n *NodeHandler) getEtcdEndpoints() ([]string, error) {
	// This is a bit janky, can be replaced when we think of something better,
	// but get list of etcd pods and their IPs, and that's your etcd endpoints, roughly
	// (TODO) can we not get proper service endpoint of etcd cluster?
	ips := []string{}
	pods, _ := n.KubeClientSet.CoreV1().Pods("kube-system").List(n.Ctx, metav1.ListOptions{
		LabelSelector: "component=etcd,tier=control-plane",
	})
	for _, pod := range pods.Items {
		ips = append(ips, fmt.Sprintf("%s:2379", pod.Status.PodIP))
	}
	if len(ips) < 1 {
		return nil, fmt.Errorf("unable to get at least one IP of etcd pods, got %d, something's not right", len(ips))
	}
	return ips, nil
}

func deleteEtcdMember(nodeIP, nodeName string, endpoints []string) error {
	// Talk to etcd and remove this node as a member.
	// Have observed that the node being removed can sometimes have etcd go unresponsive on that node,
	// so let's talk to other endpoints and exclude the node being removed.
	// TODO: Could we check each master node and see which ones are responding for etc and
	// only talk to those nodes?
	caPath := "/etc/kubernetes/pki/etcd/ca.crt"
	certPath := "/etc/kubernetes/pki/etcd/server.crt"
	keyPath := "/etc/kubernetes/pki/etcd/server.key"
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("error on loading etcd cert/key: %v", err)
	}
	caData, err := ioutil.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("error on loading etcd ca: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caData)

	etcdClient, err := etcdv3.New(etcdv3.Config{
		// Exclude node being removed
		Endpoints:   sliceExcludingString(fmt.Sprintf("%s:2379", nodeIP), endpoints),
		DialTimeout: 5 * time.Second,
		TLS: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
		},
	})
	if err != nil {
		return fmt.Errorf("error when connecting to etcd server: %v", err)
	}
	defer etcdClient.Close() // Very important, can leak otherwise
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	members, err := etcdClient.MemberList(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("error when connecting to etcd server: %v", err)
	}
	var memberID uint64
	for _, member := range members.Members {
		klog.V(3).Infof("deleteEtcdMember saw member %s with ID %v", member.GetName(), member.GetID())
		if member.GetName() == nodeName {
			memberID = member.GetID()
		}
	}
	if memberID == 0 {
		klog.Warningf("Cound not fetch member ID for %s in etcd cluster, going to assume it's already gone", nodeName)
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	_, err = etcdClient.MemberRemove(ctx, memberID)
	cancel()
	if err != nil {
		return fmt.Errorf("error delete memberID %v in etcd server: %v", memberID, err)
	}
	return nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func sliceExcludingString(a string, list []string) (r []string) {
	for _, b := range list {
		if b != a {
			r = append(r, b)
		}
	}
	return
}
