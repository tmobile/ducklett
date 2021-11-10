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
limitations under  the License.
*/

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tmobile/ducklett/clouds/azure"
	master "github.com/tmobile/ducklett/nodes/cp"
	"github.com/tmobile/ducklett/nodes/worker"

	apiv1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	kubeadmv1 "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta2"
	"sigs.k8s.io/yaml"
)

const (
	ducklettStatusString               = "ducklett-status"
	ducklettStatusUpgradeReadyString   = "upgradeReady"
	ducklettStatusUpgradeStartedString = "upgradeStarted"
	ducklettStatusReadyDrainString     = "upgradeDrainReady"
	ducklettStatusDrainStartedString   = "upgradeDrainStarted"
	ducklettStatusReadyDeleteString    = "upgradeDeleteReady"
	ducklettNewVMCountString           = "ducklett-new-vm-count"
	ducklettNewImageString             = "ducklett-upgrade-to-imageID"
	maxGracefulTerminationSec          = 360
	ducklettDrainTimestampString       = "ducklett-drain-timestamp"
	ducklettUnschedulableString        = "DucklettUnschedable"
	ducklettNoExecuteString            = "DucklettNoExecute"
)

// ControllerOptions struct holds config options
type ControllerOptions struct {
	Ctx               context.Context
	KubeClientSet     kubernetes.Interface
	ClusterName       string
	CheckPeriod       time.Duration
	CPConcurrency     int
	WorkerConcurrency int
	SubscriptionID    string
	ResourceGroup     string
	AzureVMSSClient   azure.AzureVirtualMachineScaleSetsClientAPI
	AzureVMSSVMClient azure.VirtualMachineScaleSetVMsClientAPI
}

// Controller struct holds k8s client (test2)
type Controller struct {
	Opts ControllerOptions
}

// NewController returns newly made Controller object
func NewController(opts ControllerOptions) *Controller {
	return &Controller{
		Opts: opts,
	}
}

// NodeHandler interface for worker and controlplane node types
type NodeHandler interface {
	GetConcurrencyMax() int                                                                 // Get ConcurrencyMax value for nodehandler
	GetNodeTypeName() string                                                                // Just for logs, show the type of nodes (controlplane/worker) operated on
	GetNodeList() (*apiv1.NodeList, error)                                                  // Filter nodes by label
	GetPodsToIgnore() []string                                                              // Daemon sets for workers, but what for masters? kube-api and coredns and ??? What are they?
	GetSSName(apiv1.Node) (string, error)                                                   // VMSS compare image ref, VM compares tag
	PrepForDelete(apiv1.Node, string) error                                                 // Worker placeholder, controlplane do etcd removal and kubeadm-config update
	GetVMName(string, apiv1.Node, azure.VirtualMachineScaleSetVMsClientAPI) (string, error) // Get VM name of node in VMSS
}

// Run starts the controller run loop
func (c *Controller) Run(stopCh <-chan struct{}) error {
	klog.Infof("Starting ducklett controller, checking every %v", c.Opts.CheckPeriod)

	cpNodes := master.NewNodeHandler(
		c.Opts.Ctx,
		c.Opts.ClusterName,
		c.Opts.SubscriptionID,
		c.Opts.ResourceGroup,
		c.Opts.KubeClientSet,
		c.Opts.CPConcurrency)
	workerNodes := worker.NewNodeHandler(
		c.Opts.Ctx,
		c.Opts.ClusterName,
		c.Opts.SubscriptionID,
		c.Opts.ResourceGroup,
		c.Opts.KubeClientSet,
		c.Opts.WorkerConcurrency)

	// Run the reconcile for master nodes every CheckPeriod, timer starts again once the function finishes
	masterNodeReconcile := func() {
		c.reconcileNodes(cpNodes)
	}
	go wait.Until(masterNodeReconcile, c.Opts.CheckPeriod, stopCh)

	// Run the reconcile for worker nodes every CheckPeriod, timer starts again once the function finishes
	workerNodeReconcile := func() {
		c.reconcileNodes(workerNodes)
	}
	go wait.Until(workerNodeReconcile, c.Opts.CheckPeriod, stopCh)

	klog.V(3).Info("Started controller")
	<-stopCh
	klog.V(3).Info("Shutting down controller")

	return nil
}

func (c *Controller) reconcileNodes(nodeHandler NodeHandler) {
	// Create or refresh Azure clients
	if err := c.GetAzureClients(); err != nil {
		klog.Errorf("Failed to get Azure client: %v", err)
		return
	}
	// Get list of nodes we are managing
	nodes, err := nodeHandler.GetNodeList()
	if err != nil {
		klog.Errorf("Failed to get list of %s nodes: %v", nodeHandler.GetNodeTypeName(), err)
		return
	}
	klog.V(3).Infof("Got list of %s nodes, count: %d", nodeHandler.GetNodeTypeName(), len(nodes.Items))

	// Loop through nodes, push state machine by annotations (TODO: could improve this with labels?)
	for _, node := range nodes.Items {
		annotations := node.GetAnnotations()
		if _, ok := annotations[ducklettStatusString]; ok {
			klog.V(3).Infof("Node %s has %s: %s", node.GetName(), ducklettStatusString, annotations[ducklettStatusString])

			// Our annotation is present on node, switch on value
			switch ann := annotations[ducklettStatusString]; ann {
			case ducklettStatusUpgradeStartedString:
				klog.V(3).Infof("Node %s has started upgrading, see if we are ready to drain", node.GetName())

				// Check if node is ready to start drain (ensure replacements have joined cluster)
				ssName, err := nodeHandler.GetSSName(node)
				if err != nil {
					klog.Errorf("Failed getting scaleset name of node %s, skipping: %v", node.GetName(), err)
					continue
				}
				ready, err := c.readyToDrain(ssName, node, ducklettNewVMCountString)
				if err != nil {
					klog.Errorf("Error on checking ReadyToDrain for node %s: %v", node.GetName(), err)
					continue
				}
				if !ready {
					klog.V(3).Infof("Node %s is not ReadyToDrain, skipping", node.GetName())
					continue
				}
				// Mark unsechedulable and ready to drain, will start drain next cycle
				if err = c.makeNodeReadyToDrain(node.GetName()); err != nil {
					klog.Errorf("Error on markNodeUnschedulable node %s: %v", node.GetName(), err)
					continue
				}
				klog.V(3).Infof("Setting node %s ready to drain", node.GetName())
				continue
			case ducklettStatusReadyDrainString:
				klog.V(3).Infof("Node %s is ready to drain", node.GetName())

				// TODO: should we evict pods or just mark NoExecute and let pods be kicked?
				/* 	if err = c.markNodeNoExecute(node.GetName()); err != nil {
					klog.Errorf("Error on markNodeNoExecute node %s: %v", node.GetName(), err)
					continue
				} */

				// Start drain: get pods, evict pods
				podList, err := c.getPodsForNode(node.GetName())
				if err != nil {
					klog.Errorf("Error on getting pods for node %s: %v", node.GetName(), err)
					continue
				}

				// Our own pod may get evicted here, which will prevent us from making it to annotation update, but we'll just rerun this block
				c.evictPods(podList, nodeHandler.GetPodsToIgnore())

				// Annotate node with timestamp that drain started (for force evictions)
				timestamp := fmt.Sprintf("%d", time.Now().Unix())
				drainAnnos := map[string]string{
					ducklettDrainTimestampString: timestamp,
					ducklettStatusString:         ducklettStatusDrainStartedString,
				}
				if err = c.updateNodeAnnotations(node.GetName(), drainAnnos); err != nil {
					klog.Errorf("Error on updating annotations (%s=%s) for node %s: %v", ducklettStatusString, ducklettStatusDrainStartedString, node.GetName(), err)
					continue
				}
				klog.V(3).Infof("Setting node %s drain started", node.GetName())
				continue
			case ducklettStatusDrainStartedString:
				klog.V(3).Infof("Node %s started drain, check if it's ready for deletion", node.GetName())

				// Check if node is drained
				podList, err := c.getPodsForNode(node.GetName())
				if err != nil {
					klog.Errorf("Error on getting pods for node %s: %v", node.GetName(), err)
					continue
				}
				if isNodeDrained(podList.Items, nodeHandler.GetPodsToIgnore()) {
					klog.V(3).Infof("Node %s drained, ready for deletion", node.GetName())
					imageName, ok := annotations[ducklettNewImageString]
					if !ok {
						klog.Errorf("Failed to find imageName from annotations for node %s in %s", node.GetName(), ducklettNewImageString)
						continue
					}
					if err = nodeHandler.PrepForDelete(node, imageName); err != nil {
						klog.Errorf("Error running PrepForDelete for node %s: %v", node.GetName(), err)
						continue
					}
					if err = c.updateNodeAnnotations(node.GetName(), map[string]string{ducklettStatusString: ducklettStatusReadyDeleteString}); err != nil {
						klog.Errorf("Error on updating annotations (%s=%s) for node %s: %v", ducklettStatusString, ducklettStatusReadyDeleteString, node.GetName(), err)
						continue
					}
					klog.V(3).Infof("Setting node %s drained, ready to delete", node.GetName())
				} else {
					// Check if it's time for forced evictions based on max grace time and when drain started
					if maxGraceExceeded(node.GetName(), annotations) {
						if err = c.forcePodEvictions(podList.Items, nodeHandler.GetPodsToIgnore()); err != nil {
							klog.Errorf("Error on forcePodEvictions for node %s: %v", node.GetName(), err)
						}
					} else {
						// Otherwise, run another round of evictions
						c.evictPods(podList, nodeHandler.GetPodsToIgnore())
					}
					// Continue to next reconcile and node will be rechecked for remaining pods then
					continue
				}
				continue
			case ducklettStatusReadyDeleteString:
				klog.V(3).Infof("Node %s is ready to delete", node.GetName())

				// Get scaleset name and VM name in VMSS
				ssName, err := nodeHandler.GetSSName(node)
				if err != nil {
					klog.Errorf("Error on getting scaleset name for node %s: %v", node.GetName(), err)
					continue
				}
				vmName, err := nodeHandler.GetVMName(ssName, node, c.Opts.AzureVMSSVMClient)
				if err != nil {
					klog.Errorf("Error on getting VM name for node %s: %v", node.GetName(), err)
					continue
				}
				// Check that Azure infra is not updating, skip if it is
				updating, err := c.isInfraUpdating(ssName)
				if err != nil {
					klog.Errorf("Error on checking if Azure infra is updating for node %s: %v", node.GetName(), err)
					continue
				}
				if updating {
					klog.Infof("Azure infra is updating for node %s, skip for later", node.GetName())
					continue
				}
				// Delete node from cluster
				klog.V(3).Infof("Deleting node %s from cluster", node.GetName())
				if err = c.Opts.KubeClientSet.CoreV1().Nodes().Delete(c.Opts.Ctx, node.GetName(), metav1.DeleteOptions{}); err != nil {
					klog.Errorf("Failed delete node %s from cluster: %v", node.GetName(), err)
					continue
				}
				// Call node delete from Azure
				klog.V(3).Infof("Deleting VM %s for node %s in VMSS %s", vmName, node.GetName(), ssName)
				err = azure.DeleteVMFromVMSS(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName, vmName)
				if err != nil {
					klog.Errorf("failed to call for deletion of node %s in scaleset %s for VM %s: %v", node.GetName(), ssName, vmName, err)
					continue
				}
				continue
			}
		}
	}
	// Check for nodes that are ready to be refreshed, group by scale set and scale up
	vmmsSets := make(map[string][]string)
	klog.V(3).Info("Checking if any vmss need to be scaled up")
	for _, node := range nodes.Items {
		if anno, ok := node.Annotations[ducklettStatusString]; ok && anno == ducklettStatusUpgradeReadyString {
			ssName, err := nodeHandler.GetSSName(node)
			if err != nil {
				klog.Errorf("Failed getting scaleset name of node %s, skipping: %v", node.GetName(), err)
				continue
			}
			klog.V(3).Infof("Node %s is UpgradeReady, in VMSS %s", node.GetName(), ssName)
			vmmsSets[ssName] = append(vmmsSets[ssName], node.GetName())
		}
	}
	for ssName, nodeNames := range vmmsSets {
		// Get number of VMs in scaleset
		instances, err := azure.GetInstancesInVMSS(c.Opts.Ctx, c.Opts.AzureVMSSVMClient, c.Opts.ResourceGroup, ssName)
		if err != nil {
			klog.Errorf("Failed to get number of instances in vmss %s, skipping: %v", ssName, err)
			continue
		}
		numVMs := len(instances)

		// Mark nodes with annotation to show how many nodes we want after scaleup
		newVMs := len(nodeNames)
		totalVMs := numVMs + newVMs
		klog.V(3).Infof("Number VMs found in VMSS %v, and we want to scale up by %v for total of %v", numVMs, newVMs, totalVMs)
		for _, nodeName := range nodeNames {
			err = c.updateNodeAnnotations(nodeName, map[string]string{ducklettNewVMCountString: fmt.Sprintf("%v", totalVMs)})
			if err != nil {
				klog.Errorf("Error on updating annotations for node %s, skipping: %v", nodeName, err)
				continue
			}
		}
		// Check that VMSS isn't in the middle of updating
		updating, err := c.isInfraUpdating(ssName)
		if err != nil {
			klog.Errorf("Error on checking if Azure infra is updating: %v", err)
			continue
		}
		if updating {
			klog.Warningf("Azure infra is updating for VMSS %s, skipping", ssName)
			continue
		}
		// Scale up each VMSS by amount needed
		err = azure.ScaleUpVMSS(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName, int64(newVMs))
		if err != nil {
			klog.Errorf("Error on scaling up VMSS %s: %v", ssName, err)
			continue
		}
		// Mark nodes have started upgrade
		for _, nodeName := range nodeNames {
			err = c.updateNodeAnnotations(nodeName, map[string]string{ducklettStatusString: ducklettStatusUpgradeStartedString})
			if err != nil {
				klog.Errorf("Error on updating annotations for node %s, skipping: %v", nodeName, err)
				continue
			}
		}
	}

	// Check how many updates are going, skip updating more if at max
	numUpdating := getNumberUpdating(nodes, ducklettStatusString)
	if numUpdating >= nodeHandler.GetConcurrencyMax() {
		klog.V(3).Infof("At max %s concurrent updates of %v, waiting to evaluate more nodes", nodeHandler.GetNodeTypeName(), nodeHandler.GetConcurrencyMax())
		return
	}
	// Loop through nodes, mark old nodes for refresh, don't exeed max concurrency
	for _, node := range nodes.Items {
		if numUpdating >= nodeHandler.GetConcurrencyMax() {
			klog.V(3).Infof("Now at %v %s updates with max concurrent updates of %v, skipping evaluation of more nodes", numUpdating, nodeHandler.GetNodeTypeName(), nodeHandler.GetConcurrencyMax())
			break
		}
		// Get annotations, filter out any that are in-progress, select only stale nodes
		annotations := node.GetAnnotations()
		if _, ok := annotations[ducklettStatusString]; !ok {
			// Check node is ready for refresh
			klog.V(3).Infof("Evaluating node %s for refresh", node.GetName())
			ssName, err := nodeHandler.GetSSName(node)
			if err != nil {
				klog.Errorf("Failed getting scaleset name of node %s, skipping: %v", node.GetName(), err)
				continue
			}
			needsUpdating, imageID, err := c.needsRefreshed(ssName, node)
			if err != nil {
				klog.Errorf("Error on checking for new imageID on node %s: %v", node.GetName(), err)
				continue
			}
			if needsUpdating {
				// Node out of sync with desired imageID, check that node is healthy (TODO: Increase checks here)
				klog.V(3).Infof("Node %s has imageID %s and desired imageID is %s", node.GetName(), node.Status.NodeInfo.OSImage, imageID)
				if !isNodeReady(&node) {
					klog.Warningf("Node %s is not in a healthy state, skipping evaluation for refresh", node.GetName())
					continue
				}
				// Annotate node with UpgradeReady, will have VMSS scaled up on next cycle
				newAnnos := map[string]string{
					ducklettStatusString:   ducklettStatusUpgradeReadyString,
					ducklettNewImageString: imageID,
				}
				err = c.updateNodeAnnotations(node.GetName(), newAnnos)
				if err != nil {
					klog.Errorf("Error on updating annotations for node %s: %v", node.GetName(), err)
					continue
				}
				// Update k8s version in kubeadm-config configMap (must be done before replacement nodes come online)
				if err := c.updateK8sVersionKubeadmConfigMap(imageID); err != nil {
					klog.Warningf("Failed to update kubeadm-config configMap for node %s but continuing: %v", node.GetName(), err)
				}
				numUpdating = numUpdating + 1
			} else {
				klog.V(3).Infof("Node %s has current imageID (%s) in Azure VMSS, skipping", node.GetName(), imageID)
			}
		}
	}
}

// GetAzureClients creates or refreshes the Azure clients for VMSS
func (c *Controller) GetAzureClients() error {
	vmssClient, err := azure.GetVMSSClient(c.Opts.SubscriptionID)
	if err != nil {
		return err
	}
	c.Opts.AzureVMSSClient = vmssClient

	vmssvmClient, err := azure.GetVMSSVMClient(c.Opts.SubscriptionID)
	if err != nil {
		return err
	}
	c.Opts.AzureVMSSClient = vmssClient
	c.Opts.AzureVMSSVMClient = vmssvmClient

	return nil
}

func (c *Controller) updateNodeAnnotations(nodeName string, newAnnos map[string]string) error {
	node, err := c.Opts.KubeClientSet.CoreV1().Nodes().Get(c.Opts.Ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	annotations := node.GetAnnotations()
	if annotations == nil {
		return fmt.Errorf("Got nil annotations for node %s, are we sure this node still exists in the cluster??", nodeName)
	}
	for n := range newAnnos {
		annotations[n] = newAnnos[n]
	}
	node.SetAnnotations(annotations)
	klog.V(3).Infof("Setting annotations on node %s", node.GetName())
	_, err = c.Opts.KubeClientSet.CoreV1().Nodes().Update(c.Opts.Ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// needsRefreshed checks the OS of the node vs the VMSS, returns new imageID if updated
func (c *Controller) needsRefreshed(ssName string, node apiv1.Node) (bool, string, error) {
	imageRef, err := azure.GetImageReferenceFromVMSS(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName)
	if err != nil {
		return false, "", fmt.Errorf("error on getting imageReference from VMSS: %v", err)
	}
	imageID, err := azure.GetImageIDFromImageResource(imageRef)
	if err != nil {
		return false, "", fmt.Errorf("Error on getting imageID from imageReference: %v", err)
	}
	// Check if node in sync with cloud
	if imageID == node.Status.NodeInfo.OSImage {
		return false, imageID, nil
	}
	return true, imageID, nil
}

// isInfraUpdating checks if VMSS is updating and shouldn't be touched
func (c *Controller) isInfraUpdating(ssName string) (bool, error) {
	isUpdating, err := azure.IsVMSSUpdating(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName)
	if err != nil {
		return false, fmt.Errorf("failed to get VMSS status for VMSS %s: %v", ssName, err)
	}
	return isUpdating, nil
}

// getNumberUpdating returns count of nodes in some phase of updating
func getNumberUpdating(nodes *apiv1.NodeList, annotationKey string) int {
	numUpdating := 0
	for _, node := range nodes.Items {
		if _, ok := node.Annotations[annotationKey]; ok {
			numUpdating = numUpdating + 1
		}
	}
	return numUpdating
}

func (c *Controller) getPodsForNode(nodeName string) (*apiv1.PodList, error) {
	opt := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	}
	return c.Opts.KubeClientSet.CoreV1().Pods("").List(c.Opts.Ctx, opt)
}

func isNodeDrained(pods []apiv1.Pod, ignoreList []string) bool {
	for _, pod := range pods {
		for _, oref := range pod.OwnerReferences {
			// Check kind of pod versus types to ignore
			if !stringInSlice(oref.Kind, ignoreList) {
				klog.V(3).Infof("Pod still running in node: %s", pod.Name)
				return false
			}
		}
	}
	return true
}

func (c *Controller) makeNodeReadyToDrain(nodeName string) error {
	// Get node to be marked and tainted
	node, err := c.Opts.KubeClientSet.CoreV1().Nodes().Get(c.Opts.Ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Annotate node that it's ready to drain
	annotations := node.GetAnnotations()
	if annotations == nil {
		return fmt.Errorf("Got nil annotations for node %s, are we sure this node still exists in the cluster??", nodeName)
	}
	annotations[ducklettStatusString] = ducklettStatusReadyDrainString
	node.SetAnnotations(annotations)

	// Make sure taint doesn't exist already
	for _, taint := range node.Spec.Taints {
		if taint.Key == ducklettUnschedulableString {
			klog.Warningf("Node %s already has noSchedule taint %s", nodeName, ducklettUnschedulableString)
			return nil
		}
	}
	// Append taint to list
	node.Spec.Taints = append(node.Spec.Taints, apiv1.Taint{
		Key:    ducklettUnschedulableString,
		Value:  fmt.Sprint(time.Now().Unix()),
		Effect: apiv1.TaintEffectNoSchedule,
	})
	node.Spec.Unschedulable = true

	// Update node with changes
	_, err = c.Opts.KubeClientSet.CoreV1().Nodes().Update(c.Opts.Ctx, node, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Error while marking node %s with ReadyToDrain and NoSchedule taint: %v", nodeName, err)
		return err
	}
	klog.V(3).Infof("Successfully marked node %s with ReadyToDrain and NoSchedule taint", nodeName)
	return nil
}

func (c *Controller) markNodeNoExecute(nodeName string) error {
	// Get node to be tainted
	node, err := c.Opts.KubeClientSet.CoreV1().Nodes().Get(c.Opts.Ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Make sure taint doesn't exist already
	for _, taint := range node.Spec.Taints {
		if taint.Key == ducklettNoExecuteString {
			klog.Warningf("Node %s already has noExecute taint %s", nodeName, ducklettNoExecuteString)
			return nil
		}
	}
	// Append taint to list
	node.Spec.Taints = append(node.Spec.Taints, apiv1.Taint{
		Key:    ducklettNoExecuteString,
		Value:  fmt.Sprint(time.Now().Unix()),
		Effect: apiv1.TaintEffectNoExecute,
	})

	// Update node
	_, err = c.Opts.KubeClientSet.CoreV1().Nodes().Update(c.Opts.Ctx, node, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Error while adding noExecute taint on node %s: %v", nodeName, err)
		return err
	}
	klog.V(3).Infof("Successfully added noExecute taint on node %s", nodeName)
	return nil
}

// readyToDrain checks if the replacement nodes are up and in-cluster, which means it's safe to proceed with draining old nodes
func (c *Controller) readyToDrain(ssName string, node apiv1.Node, keyName string) (bool, error) {
	// Check we have replacement instance count for VMSS
	wantedNumVMsString, ok := node.GetAnnotations()[keyName]
	if !ok {
		return false, fmt.Errorf("failed to find replacement instance count for VMSS in annotations (key=%s)", keyName)
	}
	wantedNumVMs, err := strconv.Atoi(wantedNumVMsString)
	if err != nil {
		return false, fmt.Errorf("failed to convert annotation string %s to integer: %v", wantedNumVMsString, err)
	}
	// Check if we have at least this number of VMs in VMSS
	instances, err := azure.GetInstancesInVMSS(c.Opts.Ctx, c.Opts.AzureVMSSVMClient, c.Opts.ResourceGroup, ssName)
	if err != nil {
		return false, fmt.Errorf("failed to get number of instances in vmss %s: %v", ssName, err)
	}
	numVMs := len(instances)
	if numVMs < wantedNumVMs {
		klog.V(3).Infof("failed to see VMSS %s increase in size yet, wanted %d but at %d", ssName, wantedNumVMs, numVMs)
		return false, nil
	}
	// Check that all instances have Succeeded and none of the them are Failed
	for vmName, details := range instances {
		if details["state"] == "Failed" {
			// Instance has failed, try to delete it and (currently) have to trigger VMSS scaleup again
			klog.Warningf("Replacement VM %s in VMSS %s in Failed state, trying to delete and recreate", details["instanceID"], ssName)

			err = azure.DeleteVMFromVMSS(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName, vmName)
			if err != nil {
				klog.Errorf("failed to call for deletion of node %s in scaleset %s for VM %s: %v", node.GetName(), ssName, details["instanceID"], err)
				continue
			}
			err = azure.ScaleUpVMSS(c.Opts.Ctx, c.Opts.AzureVMSSClient, c.Opts.ResourceGroup, ssName, int64(wantedNumVMs))
			if err != nil {
				klog.Errorf("Error on scaling up VMSS %s: %v", ssName, err)
				continue
			}
			/* 			azure.RestartVMInVMSS(c.Opts.Ctx, c.Opts.AzureVMSSVMClient, c.Opts.ResourceGroup, ssName, details["id"])
			   			if err != nil {
			   				return false, fmt.Errorf("replacement VM %s in VMSS %s failed to deploy, and failed to restart: %v", vmName, ssName, err)
			   			}
			   			return false, fmt.Errorf("replacement VM %s in VMSS %s failed to deploy, attempting to restart", vmName, ssName) */
		}
		if details["state"] == "Creating" {
			klog.V(3).Infof("Replacement VM %s in VMSS %s still creating", vmName, ssName)
			return false, nil
		}
	}
	// Check that all the machines in VMSS have joined the cluster and are Ready
	nodes, err := c.Opts.KubeClientSet.CoreV1().Nodes().List(c.Opts.Ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	nodesFound := []string{}
	for _, node := range nodes.Items {
		for vmName := range instances {
			if node.GetName() == vmName {
				klog.V(3).Infof("readyToDrain found node %s in instance list", node.GetName())
				if !isNodeReady(&node) {
					klog.V(3).Infof("replacement node %s is not Ready yet", node.GetName())
					return false, nil
				}
				nodesFound = append(nodesFound, node.GetName())
			}
		}
	}
	if len(nodesFound) < wantedNumVMs {
		missingNodes := diffSlice(nodesFound, instances)
		klog.V(3).Infof("failed to see new nodes %v in cluster yet, wanted %d but at %d", strings.Join(missingNodes, ", "), wantedNumVMs, len(nodesFound))
		return false, nil
	}
	klog.V(3).Infof("all %d replacement nodes found in cluster and Ready", wantedNumVMs)
	return true, nil
}

func (c *Controller) evictPods(podList *apiv1.PodList, ignoreList []string) {
	// Eviction policy respects PodDisruptionBudget: https://kubernetes.io/docs/concepts/scheduling-eviction/api-eviction/
	klog.V(3).Infof("evictPods called on %d pods", len(podList.Items))
	for _, pod := range podList.Items {
		excludePod := false
		for _, oref := range pod.OwnerReferences {
			// Check kind of pod versus types to ignore
			if stringInSlice(oref.Kind, ignoreList) {
				excludePod = true
				break
			}
		}
		if excludePod {
			klog.V(3).Infof("Skipping eviction of pod %s", pod.Name)
			continue
		}
		klog.V(3).Infof("Attempting to evict pod %s", pod.Name)
		// If there's no termination set, take the default, otherwise take the lesser of
		// maxGracefulTerminationSec and the pods termination period
		gracePeriod := int64(apiv1.DefaultTerminationGracePeriodSeconds)
		if pod.Spec.TerminationGracePeriodSeconds != nil {
			if *pod.Spec.TerminationGracePeriodSeconds < int64(maxGracefulTerminationSec) {
				gracePeriod = *pod.Spec.TerminationGracePeriodSeconds
			} else {
				gracePeriod = int64(maxGracefulTerminationSec)
			}
		}
		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pod.Namespace,
				Name:      pod.Name,
			},
			DeleteOptions: &metav1.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			},
		}
		err := c.Opts.KubeClientSet.CoreV1().Pods(pod.Namespace).Evict(c.Opts.Ctx, eviction)
		if err != nil {
			klog.Warningf("Got error on pod %s eviction: %v", pod.Name, err)
		} else {
			klog.V(3).Infof("Successfully filed eviction for pod %s", pod.Name)
		}
	}
}

func (c *Controller) forcePodEvictions(pods []apiv1.Pod, ignoreList []string) error {
	klog.V(3).Infof("forcePodevictions called on %d pods", len(pods))
	for _, pod := range pods {
		excludePod := false
		for _, oref := range pod.OwnerReferences {
			// Check kind of pod versus types to ignore
			if stringInSlice(oref.Kind, ignoreList) {
				excludePod = true
				break
			}
		}
		if excludePod {
			klog.V(3).Infof("Skipping forcePodEvictions of pod %s", pod.Name)
			continue
		}
		err := c.Opts.KubeClientSet.CoreV1().Pods(pod.Namespace).Delete(c.Opts.Ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("Error deleting pod %s: %v", pod.Name, err)
			return err
		}
	}
	return nil
}

// updateK8sVersionKubeadmConfigMap updates k8s version in ClusterConfiguration
func (c *Controller) updateK8sVersionKubeadmConfigMap(imageName string) error {
	k8sVersion, err := k8sVersionFromImageName(imageName)
	if err != nil {
		return err
	}
	cm, err := c.Opts.KubeClientSet.CoreV1().ConfigMaps("kube-system").Get(c.Opts.Ctx, "kubeadm-config", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if configYAML, ok := cm.Data["ClusterConfiguration"]; ok {
		clusterConfig := kubeadmv1.ClusterConfiguration{}
		err := yaml.Unmarshal([]byte(configYAML), &clusterConfig)
		if err != nil {
			return fmt.Errorf("failed to decode ClusterConfiguration in data of kubeadm-config configMap: %v", err)
		}
		// Return early if no update needed
		if clusterConfig.KubernetesVersion == k8sVersion {
			klog.V(3).Infof("ClusterConfiguration in kubeadm-config configMap already has k8s version %s", k8sVersion)
			return nil
		}
		clusterConfig.KubernetesVersion = fmt.Sprintf("v%s", k8sVersion)
		configBytes, err := yaml.Marshal(clusterConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal ClusterConfiguration back to yaml: %v", err)
		}
		cm.Data["ClusterConfiguration"] = string(configBytes)
	} else {
		return fmt.Errorf("failed to find ClusterConfiguration in data of kubeadm-config configMap")
	}
	// Write the updated configMap back to the cluster
	klog.V(3).Infof("Updating k8s version to %s in kubeadm-config configMap", k8sVersion)
	_, err = c.Opts.KubeClientSet.CoreV1().ConfigMaps("kube-system").Update(c.Opts.Ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func maxGraceExceeded(nodeName string, anno map[string]string) bool {
	if timestamp, ok := anno[ducklettDrainTimestampString]; ok {
		timestampInt, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			klog.Errorf("Error converting timestamp %s to int64: %v", timestamp, err)
			return false
		}
		if time.Now().Unix() > timestampInt+maxGracefulTerminationSec {
			return true
		}
	} else {
		klog.Warningf("No max grace time found for node %s", nodeName)
	}
	return false
}

func isNodeReady(node *apiv1.Node) bool {
	// Check status of node object to see if condition is Ready
	if node == nil {
		return false
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == apiv1.NodeReady && condition.Status == apiv1.ConditionTrue {
			return true
		}
	}
	return false
}

func k8sVersionFromImageName(imageName string) (string, error) {
	// image name format: conducktor-flatcar-1.20.11-v0.4.0-v0.0.1
	a := strings.Split(imageName, "-")
	if len(a) != 5 {
		return "", fmt.Errorf("Failed to split imageName into proper number of pieces: %s", imageName)
	}
	if a[2] == "" {
		return "", fmt.Errorf("imageName had empty values for k8s version: %s", imageName)
	}
	return a[2], nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func diffSlice(exclude []string, list map[string]map[string]string) (r []string) {
	for n := range list {
		if !stringInSlice(n, exclude) {
			r = append(r, n)
		}
	}
	return
}
