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
package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kubeadmv1 "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta2"
	"sigs.k8s.io/yaml"
)

var (
	controller = NewController(ControllerOptions{})
)

func TestIsNodeDrained(t *testing.T) {
	tests := []struct {
		inPods   []apiv1.Pod
		inIgnore []string
		name     string
		want     bool
	}{
		{
			name: "test with all daemonsets",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "DaemonSet",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "DaemonSet",
							},
						},
					},
				},
			},
			inIgnore: []string{"DaemonSet"},
			want:     true,
		},
		{
			name: "test with no daemonsets",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
							},
						},
					},
				},
			},
			inIgnore: []string{"DaemonSet"},
			want:     false,
		},
		{
			name: "test with some daemonsets",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "DaemonSet",
							},
						},
					},
				},
			},
			inIgnore: []string{"DeamonSet"},
			want:     false,
		},
		{
			name: "test with some nodes",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "Node",
							},
						},
					},
				},
			},
			inIgnore: []string{"DeamonSet", "Node"},
			want:     false,
		},
		{
			name: "test with all node",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "Node",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "Node",
							},
						},
					},
				},
			},
			inIgnore: []string{"DeamonSet", "Node"},
			want:     true,
		},
		{
			name: "test with mix node and daemonsets",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "Node",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "DaemonSet",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod3",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
							},
						},
					},
				},
			},
			inIgnore: []string{"DeamonSet", "Node"},
			want:     false,
		},
		{
			name: "test with mix node and daemonsets",
			inPods: []apiv1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod1",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "Node",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testpod2",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "DaemonSet",
							},
						},
					},
				},
			},
			inIgnore: []string{"DaemonSet", "Node"},
			want:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isNodeDrained(tc.inPods, tc.inIgnore)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestUpdateNodeAnnotation(t *testing.T) {
	data := []struct {
		clientset     kubernetes.Interface
		nodeName      string
		newAnnos      map[string]string
		expectedData  map[string]string
		expectedError bool
		name          string
	}{
		{
			name:     "Node has correct data",
			nodeName: "testcluster-cp0",
			newAnnos: map[string]string{ducklettStatusString: ducklettStatusUpgradeStartedString},
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp0",
						Annotations: map[string]string{
							"some-other-annotation": "anything",
						},
					},
				}),
			expectedError: false,
			expectedData: map[string]string{
				"some-other-annotation": "anything",
				ducklettStatusString:    ducklettStatusUpgradeStartedString,
			},
		},
		{
			name:     "Node missing annotations",
			nodeName: "testcluster-cp0",
			newAnnos: map[string]string{ducklettStatusString: ducklettStatusUpgradeStartedString},
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp0",
					},
				}),
			expectedError: true,
		},
		{
			name:     "Node does not exist",
			nodeName: "testcluster-cp0",
			newAnnos: map[string]string{ducklettStatusString: ducklettStatusUpgradeStartedString},
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-wrong-node",
						Annotations: map[string]string{
							"some-other-annotation": "anything",
						},
					},
				}),
			expectedError: true,
		},
		{
			name:     "Node has correct data, many annotations",
			nodeName: "testcluster-cp0",
			newAnnos: map[string]string{ducklettStatusString: ducklettStatusUpgradeStartedString},
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp0",
						Annotations: map[string]string{
							"some-other-annotation": "anything",
							"more-other-annotation": "anything2",
							"moar-other-annotation": "so-muk-moar",
						},
					},
				}),
			expectedError: false,
			expectedData: map[string]string{
				"some-other-annotation": "anything",
				"more-other-annotation": "anything2",
				"moar-other-annotation": "so-muk-moar",
				ducklettStatusString:    ducklettStatusUpgradeStartedString,
			},
		},
		{
			name:     "Node has correct data, multiple new annos",
			nodeName: "testcluster-cp0",
			newAnnos: map[string]string{
				ducklettStatusString:   ducklettStatusUpgradeStartedString,
				ducklettNewImageString: "conducktor-flatcar-1.18.12-v0.2.1-bbb6b443",
			},
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp0",
						Annotations: map[string]string{
							"some-other-annotation": "anything",
							"more-other-annotation": "anything2",
							"moar-other-annotation": "so-muk-moar",
						},
					},
				}),
			expectedError: false,
			expectedData: map[string]string{
				"some-other-annotation": "anything",
				"more-other-annotation": "anything2",
				"moar-other-annotation": "so-muk-moar",
				ducklettStatusString:    ducklettStatusUpgradeStartedString,
				ducklettNewImageString:  "conducktor-flatcar-1.18.12-v0.2.1-bbb6b443",
			},
		},
	}

	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset     kubernetes.Interface
			nodeName      string
			newAnnos      map[string]string
			expectedData  map[string]string
			expectedError bool
			name          string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				controller.Opts.KubeClientSet = single.clientset
				err := controller.updateNodeAnnotations(single.nodeName, single.newAnnos)
				if single.expectedError && err == nil {
					t.Fatal("expected error but there was none")
				}
				if !single.expectedError && err != nil {
					t.Fatalf("not expecting error but got: %v", err)
				}
				if err == nil {
					node, _ := controller.Opts.KubeClientSet.CoreV1().Nodes().Get(context.TODO(), single.nodeName, metav1.GetOptions{})
					if !reflect.DeepEqual(single.expectedData, node.GetAnnotations()) {
						t.Fatalf("expected result of %v, got %v", single.expectedData, node.GetAnnotations())
					}
				}
			}
		}(single))
	}
}

func TestEvictPods(t *testing.T) {
	data := []struct {
		clientset     kubernetes.Interface
		nodeName      string
		annoKey       string
		annoValue     string
		expectedData  map[string]string
		expectedError bool
		name          string
	}{
		{
			name:      "Node has correct data",
			nodeName:  "testcluster-cp0",
			annoKey:   ducklettStatusString,
			annoValue: ducklettStatusUpgradeStartedString,
			clientset: fake.NewSimpleClientset(
				&apiv1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testpod1",
						Namespace: "testns",
					},
					Spec: apiv1.PodSpec{
						NodeName: "testcluster-cp0",
					},
				}),
			expectedError: false,
			expectedData: map[string]string{
				"some-other-annotation": "anything",
				ducklettStatusString:    ducklettStatusUpgradeStartedString,
			},
		},
	}

	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset     kubernetes.Interface
			nodeName      string
			annoKey       string
			annoValue     string
			expectedData  map[string]string
			expectedError bool
			name          string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				controller.Opts.KubeClientSet = single.clientset
				podList, err := controller.getPodsForNode(single.nodeName)
				if err != nil {
					t.Fatalf("error getting pods from getPodsForNode: %v", err)
				}
				//fmt.Println("podList:", podList)
				controller.evictPods(podList, []string{"DaemonSet", "Node"})
				//fmt.Println("clientset object now:", single.clientset)

				// Hmmmmmm......how to find Eviction policy objects now???

				/* 				   				for _, pod := range podList.Items {

				   				   				}
				   				   				if !reflect.DeepEqual(single.expectedData, node.GetAnnotations()) {
				   				   					t.Fatalf("expected result of %v, got %v", single.expectedData, node.GetAnnotations())
				   				   				}  */

			}
		}(single))
	}
}

func TestMaxGraceExceeded(t *testing.T) {
	data := []struct {
		clientset   kubernetes.Interface
		nodeName    string
		expectedVal bool
		name        string
	}{
		{
			name:     "Node has not expired max time, now",
			nodeName: "testnode1",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode1",
						Annotations: map[string]string{
							ducklettDrainTimestampString: fmt.Sprintf("%d", time.Now().Unix()),
						},
					},
				}),
			expectedVal: false,
		},
		{
			name:     "Node has not expired max time, now - maxGracefulTerminationSec",
			nodeName: "testnode2",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode2",
						Annotations: map[string]string{
							ducklettDrainTimestampString: fmt.Sprintf("%d", time.Now().Add(time.Second*maxGracefulTerminationSec*-1).Unix()),
						},
					},
				}),
			expectedVal: false,
		},
		{
			name:     "Node has not expired max time, now - .5 maxGracefulTerminationSec",
			nodeName: "testnode3",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode3",
						Annotations: map[string]string{
							ducklettDrainTimestampString: fmt.Sprintf("%d", time.Now().Add(time.Second*(maxGracefulTerminationSec*.5)*-1).Unix()),
						},
					},
				}),
			expectedVal: false,
		},
		{
			name:     "Node has expired max time, now - maxGracefulTerminationSec + 1",
			nodeName: "testnode4",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode4",
						Annotations: map[string]string{
							ducklettDrainTimestampString: fmt.Sprintf("%d", time.Now().Add(time.Second*(maxGracefulTerminationSec+1)*-1).Unix()),
						},
					},
				}),
			expectedVal: true,
		},
		{
			name:     "Node has expired max time, now - maxGracefulTerminationSec + 10",
			nodeName: "testnode5",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode5",
						Annotations: map[string]string{
							ducklettDrainTimestampString: fmt.Sprintf("%d", time.Now().Add(time.Second*(maxGracefulTerminationSec+10)*-1).Unix()),
						},
					},
				}),
			expectedVal: true,
		},
		{
			name:     "Node has no ducklettDrainTimestampString annotation, should skip",
			nodeName: "testnode6",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnode6",
					},
				}),
			expectedVal: false,
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset   kubernetes.Interface
			nodeName    string
			expectedVal bool
			name        string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				controller.Opts.KubeClientSet = single.clientset
				node, err := controller.Opts.KubeClientSet.CoreV1().Nodes().Get(context.TODO(), single.nodeName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("error getting nodes: %v", err)
				}
				annotations := node.GetAnnotations()
				val := maxGraceExceeded(single.nodeName, annotations)
				if val != single.expectedVal {
					t.Fatalf("expected result of %v, got %v", single.expectedVal, val)
				}

			}
		}(single))
	}
}

/* func TestMarkNodeUnschedulable(t *testing.T) {
	nodeWithTaint := func(name string, taints []apiv1.Taint) *apiv1.Node {
		return &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: apiv1.NodeSpec{
				Taints: taints,
			},
		}
	}

	getNode := func(t *testing.T, client kubernetes.Interface, name string) *apiv1.Node {
		t.Helper()
		node, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		return node
	}

	taintKey := "AzureNodeRefresherUnschedable"

	t.Run("happy_path", func(t *testing.T) {
		client := fake.NewSimpleClientset(
			nodeWithTaint("node1", []apiv1.Taint{}),
			nodeWithTaint("node2", []apiv1.Taint{}),
		)
		c := NewController(ControllerOptions{
			Ctx:           context.Background(),
			KubeClientSet: client,
		})

		err := c.markNodeUnschedulable("node1")
		require.NoError(t, err)

		n1 := getNode(t, client, "node1")
		require.Equal(t, 1, len(n1.Spec.Taints))
		require.Equal(t, taintKey, n1.Spec.Taints[0].Key)
		require.Equal(t, apiv1.TaintEffectNoSchedule, n1.Spec.Taints[0].Effect)
		require.Equal(t, true, n1.Spec.Unschedulable)

		n2 := getNode(t, client, "node2")
		require.Equal(t, 0, len(n2.Spec.Taints))
		require.Equal(t, false, n2.Spec.Unschedulable)
	})

	t.Run("taint_already_present", func(t *testing.T) {
		client := fake.NewSimpleClientset(
			nodeWithTaint("node1", []apiv1.Taint{
				{Key: taintKey, Effect: apiv1.TaintEffectNoSchedule},
			}),
		)
		c := NewController(ControllerOptions{
			Ctx:           context.Background(),
			KubeClientSet: client,
		})

		err := c.markNodeUnschedulable("node1")
		require.NoError(t, err)

		n1 := getNode(t, client, "node1")
		require.Equal(t, 1, len(n1.Spec.Taints))
		require.Equal(t, taintKey, n1.Spec.Taints[0].Key)
		require.Equal(t, apiv1.TaintEffectNoSchedule, n1.Spec.Taints[0].Effect)
	})

	t.Run("get_error", func(t *testing.T) {
		getErr := errors.New("whoops")
		client := fake.NewSimpleClientset(
			nodeWithTaint("node1", []apiv1.Taint{}),
		)
		client.CoreV1().(*fakecorev1.FakeCoreV1).PrependReactor("get", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, &apiv1.Node{}, getErr
		})
		c := NewController(ControllerOptions{
			Ctx:           context.Background(),
			KubeClientSet: client,
		})

		err := c.markNodeUnschedulable("node1")
		require.Error(t, err)
		require.ErrorIs(t, err, getErr)

		// Pop the reactor we just added so getNode doesn't error
		client.ReactionChain = client.ReactionChain[1:]
		n1 := getNode(t, client, "node1")
		require.Equal(t, 0, len(n1.Spec.Taints))
	})

	t.Run("update_error", func(t *testing.T) {
		updateErr := errors.New("whoops")
		client := fake.NewSimpleClientset(
			nodeWithTaint("node1", []apiv1.Taint{}),
		)
		client.CoreV1().(*fakecorev1.FakeCoreV1).PrependReactor("update", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, &apiv1.Node{}, updateErr
		})
		c := NewController(ControllerOptions{
			Ctx:           context.Background(),
			KubeClientSet: client,
		})

		err := c.markNodeUnschedulable("node1")
		require.Error(t, err)
		require.ErrorIs(t, err, updateErr)

		// Pop the reactor we just added so getNode doesn't error
		client.ReactionChain = client.ReactionChain[1:]
		n1 := getNode(t, client, "node1")
		require.Equal(t, 0, len(n1.Spec.Taints))
	})
} */

func TestIsNodeReady(t *testing.T) {
	data := []struct {
		node        *apiv1.Node
		expectedVal bool
		name        string
	}{
		{
			name: "Node is not ready",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testcluster-cp0",
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Message:            "kubelet is posting ready status",
							Reason:             "KubeletReady",
							Status:             apiv1.ConditionFalse,
							Type:               apiv1.NodeReady,
						},
					},
				},
			},
			expectedVal: false,
		},
		{
			name: "Node is not ready",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testcluster-cp0",
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Message:            "kubelet has sufficient PID available",
							Reason:             "KubeletHasSufficientPID",
							Status:             apiv1.ConditionFalse,
							Type:               apiv1.NodePIDPressure,
						},
						{
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Message:            "kubelet is posting ready status",
							Reason:             "KubeletReady",
							Status:             apiv1.ConditionFalse,
							Type:               apiv1.NodeReady,
						},
					},
				},
			},
			expectedVal: false,
		},
		{
			name: "Node is ready",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testcluster-cp0",
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Message:            "kubelet has sufficient PID available",
							Reason:             "KubeletHasSufficientPID",
							Status:             apiv1.ConditionFalse,
							Type:               apiv1.NodePIDPressure,
						},
						{
							LastHeartbeatTime:  metav1.Time{},
							LastTransitionTime: metav1.Time{},
							Message:            "kubelet is posting ready status",
							Reason:             "KubeletReady",
							Status:             apiv1.ConditionTrue,
							Type:               apiv1.NodeReady,
						},
					},
				},
			},
			expectedVal: true,
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			node        *apiv1.Node
			expectedVal bool
			name        string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				if result := isNodeReady(single.node); single.expectedVal != result {
					t.Fatalf("expected result of %v, got %v", single.expectedVal, result)
				}
			}
		}(single))
	}
}

func TestGetNumberUpdating(t *testing.T) {
	data := []struct {
		clientset kubernetes.Interface
		updating  int
		name      string
	}{
		{
			name: "2 nodes, 2 updating",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp0",
						Annotations: map[string]string{
							ducklettStatusString: "",
						},
					},
				}, &apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp2",
						Annotations: map[string]string{
							ducklettStatusString: "",
						},
					},
				}),
			updating: 2,
		},
		{
			name: "3 nodes, none updating",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp1",
					},
				}, &apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp2",
					},
				}, &apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp3",
					},
				}),
			updating: 0,
		},
		{
			name: "3 nodes, 3 updating",
			clientset: fake.NewSimpleClientset(
				&apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp1",
						Annotations: map[string]string{
							ducklettStatusString: "",
						},
					},
				}, &apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp2",
						Annotations: map[string]string{
							ducklettStatusString: "",
						},
					},
					Spec: apiv1.NodeSpec{
						Unschedulable: true,
					},
				}, &apiv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testcluster-cp3",
						Annotations: map[string]string{
							ducklettStatusString: "",
						},
					},
				}),
			updating: 3,
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset kubernetes.Interface
			updating  int
			name      string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				controller.Opts.KubeClientSet = single.clientset
				nodes, _ := controller.Opts.KubeClientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
				result := getNumberUpdating(nodes, ducklettStatusString)
				if single.updating != result {
					t.Fatalf("expected result of %v, got %v", single.updating, result)
				}
			}
		}(single))
	}
}

// k8sVersionFromImageName(imageName string) (string, error)
func TestK8sVersionFromImageName(t *testing.T) {
	data := []struct {
		imageName  string
		k8sVersion string
		name       string
	}{
		{
			imageName:  "conducktor-flatcar-1.18.12-v0.2.1-bbb6b443",
			k8sVersion: "1.18.12",
			name:       "image name with 1.18.12",
		},
		{
			imageName:  "conducktor-flatcar-1.19.2-v0.3.1-bbb6b443",
			k8sVersion: "1.19.2",
			name:       "image name with 1.19.2",
		},
		{
			imageName:  "conducktor-flatcar-1.21.10-v0.4.4-bbb6b443",
			k8sVersion: "1.21.10",
			name:       "image name with 1.21.10",
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			imageName  string
			k8sVersion string
			name       string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				result, err := k8sVersionFromImageName(single.imageName)
				if err != nil {
					t.Fatalf("failed: %v", err)
				}
				if single.k8sVersion != result {
					t.Fatalf("expected result of %v, got %v", single.k8sVersion, result)
				}
			}
		}(single))
	}
}

func TestUpdateK8sVersionKubeadmConfigMap(t *testing.T) {
	data := []struct {
		clientset    kubernetes.Interface
		imageName    string
		wantedConfig string
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
			wantedConfig: "v1.18.12",
			imageName:    "conducktor-flatcar-1.18.12-v0.2.1-v0.0.1",
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
			wantedConfig: "v1.19.2",
			imageName:    "conducktor-flatcar-1.19.2-v0.3.1-v0.0.1",
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
			wantedConfig: "v1.21.5",
			imageName:    "conducktor-flatcar-1.21.5-v0.5.8-v0.0.1",
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
			wantedConfig: "v1.20.8",
			imageName:    "conducktor-flatcar-1.20.8-v0.3.0-v0.0.1",
			name:         "test-node-4, doesn't exist no-op, with 1.20.8",
		},
	}
	for _, single := range data {
		t.Run(single.name, func(single struct {
			clientset    kubernetes.Interface
			imageName    string
			wantedConfig string
			name         string
		}) func(t *testing.T) {
			return func(t *testing.T) {
				controller.Opts.KubeClientSet = single.clientset
				err := controller.updateK8sVersionKubeadmConfigMap(single.imageName)
				if err != nil {
					t.Fatalf("failed: %v", err)
				}
				cm, err := controller.Opts.KubeClientSet.CoreV1().ConfigMaps("kube-system").Get(controller.Opts.Ctx, "kubeadm-config", metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed: %v", err)
				}
				if config, ok := cm.Data["ClusterConfiguration"]; ok {
					clusterConfig := kubeadmv1.ClusterConfiguration{}
					err := yaml.Unmarshal([]byte(config), &clusterConfig)
					if err != nil {
						t.Fatalf("failed: %v", err)
					}
					if single.wantedConfig != clusterConfig.KubernetesVersion {
						t.Fatalf("expected result of %v, got %v", single.wantedConfig, clusterConfig.KubernetesVersion)
					}
				} else {
					t.Fatalf("failed, missing ClusterConfiguration in configMap data")
				}
			}
		}(single))
	}
}
