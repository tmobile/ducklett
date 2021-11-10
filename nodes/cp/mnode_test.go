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
package master

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	azureNodeRefresherStatusString = "ducklett-status"
	maxConcurrentUpdates           = 1
)

var (
	handler = NewNodeHandler(context.TODO(), "testcluster", "testSubID", "testRG", nil, 3)
)

// (n *NodeHandler) getEtcdEndpoints() ([]string, error)
/* pods, _ := n.KubeClientSet.CoreV1().Pods("kube-system").List(n.Ctx, metav1.ListOptions{
	LabelSelector: "component=etcd,tier=control-plane",
})
for _, pod := range pods.Items {
	ips = append(ips, fmt.Sprintf("%s:2379", pod.Status.PodIP)) */

// getIPofNode(addys []apiv1.NodeAddress) string
func TestGetIPofNode(t *testing.T) {
	tests := []struct {
		addresses []apiv1.NodeAddress
		name      string
		wantIP    string
	}{
		{
			name: "test address 1.2.3.4",
			addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: "1.2.3.4",
				},
			},
			wantIP: "1.2.3.4",
		},
		{
			name: "test address 4.3.2.1",
			addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: "4.3.2.1",
				},
			},
			wantIP: "4.3.2.1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getIPofNode(tc.addresses)
			require.Equal(t, tc.wantIP, got)
		})
	}
}

func TestUpdateKubeadmConfigMapRemoveNode(t *testing.T) {
	data := []struct {
		clientset    kubernetes.Interface
		nodeName     string
		wantedStatus string
		name         string
	}{
		{
			clientset: fake.NewSimpleClientset(
				&apiv1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubeadm-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ClusterConfiguration": "kind: ClusterConfiguration\nkubernetesVersion: v1.18.10",
						"ClusterStatus":        "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
					},
				}),
			wantedStatus: "apiEndpoints:\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
			nodeName:     "test-node-1",
			name:         "test-node-1 with 1.18.12",
		},
		{
			clientset: fake.NewSimpleClientset(
				&apiv1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubeadm-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ClusterConfiguration": "kind: ClusterConfiguration\nkubernetesVersion: v1.19.1",
						"ClusterStatus":        "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
					},
				}),
			wantedStatus: "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
			nodeName:     "test-node-2",
			name:         "test-node-2 with 1.19.2",
		},
		{
			clientset: fake.NewSimpleClientset(
				&apiv1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubeadm-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ClusterConfiguration": "kind: ClusterConfiguration\nkubernetesVersion: v1.21.2",
						"ClusterStatus":        "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
					},
				}),
			wantedStatus: "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n",
			nodeName:     "test-node-3",
			name:         "test-node-3 with 1.21.5",
		},
		{
			clientset: fake.NewSimpleClientset(
				&apiv1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubeadm-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ClusterConfiguration": "kind: ClusterConfiguration\nkubernetesVersion: v1.20.3",
						"ClusterStatus":        "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
					},
				}),
			wantedStatus: "apiEndpoints:\n  test-node-1:\n    advertiseAddress: 10.77.34.15\n    bindPort: 6443\n  test-node-2:\n    advertiseAddress: 10.77.34.16\n    bindPort: 6443\n  test-node-3:\n    advertiseAddress: 10.77.34.17\n    bindPort: 6443\n",
			nodeName:     "test-node-4",
			name:         "test-node-4, doesn't exist no-op, with 1.20.8",
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset    kubernetes.Interface
			nodeName     string
			wantedStatus string
			name         string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				handler.KubeClientSet = single.clientset
				err := handler.updateKubeadmConfigMapRemoveNode(single.nodeName)
				if err != nil {
					t.Fatalf("failed: %v", err)
				}
				cm, err := handler.KubeClientSet.CoreV1().ConfigMaps("kube-system").Get(handler.Ctx, "kubeadm-config", metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed: %v", err)
				}
				if status, ok := cm.Data["ClusterStatus"]; ok {
					if single.wantedStatus != status {
						t.Fatalf("expected result of %v, got %v", single.wantedStatus, status)
					}
				} else {
					t.Fatalf("failed, missing ClusterStatus in configMap data")
				}
			}
		}(single))
	}
}
