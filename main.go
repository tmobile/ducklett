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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	klog "k8s.io/klog/v2"

	"github.com/tmobile/ducklett/version"
)

var (
	checkPeriod       = flag.Int64("check-period", 300, "Check period in seconds")
	workerConcurrency = flag.Int("worker-concurrency", 2, "Concurrent worker node updates allowed")
	cpConcurrency     = flag.Int("cp-concurrency", 3, "Concurrent controlplane node updates allowed")
)

// registerSignalHandlers sets up a channel to watch for exit signals
func registerSignalHandlers() (stopCh <-chan struct{}) {
	stop := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT)
	klog.Info("Registered cleanup signal handler")

	go func() {
		<-sigs
		klog.Info("Received signal, stopping controller")
		close(stop)
		<-sigs
		klog.Info("Received second signal, exiting immediately")
		klog.Flush()
		os.Exit(1)
	}()
	return stop
}

// getClientOrDie returns a k8s clientset to the request from inside the cluster
func getClientOrDie() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Error(err, "Can not get kubernetes config")
		fmt.Printf("This controller requires the in-cluster kubeconfig! Error: %v\n", err)
		os.Exit(1)
	}
	return kubernetes.NewForConfigOrDie(config)
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	klog.Infof("%s", version.DucklettASCII)
	klog.Infof("Version: %s", version.VersionString)

	subID := os.Getenv("ARM_SUBSCRIPTION_ID")
	if subID == "" {
		klog.Fatal("Missing env ARM_SUBSCRIPTION_ID")
	}
	rGroup := os.Getenv("ARM_RESOURCE_GROUP")
	if rGroup == "" {
		klog.Fatal("Missing env ARM_RESOURCE_GROUP")
	}
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		klog.Fatal("Missing env CLUSTER_NAME")
	}

	stopCh := registerSignalHandlers()
	kubeclient := getClientOrDie()

	ctx := context.TODO()
	controllerOpts := ControllerOptions{
		Ctx:               ctx,
		KubeClientSet:     kubeclient,
		ClusterName:       clusterName,
		CheckPeriod:       time.Duration(*checkPeriod * int64(time.Second)),
		CPConcurrency:     *cpConcurrency,
		WorkerConcurrency: *workerConcurrency,
		SubscriptionID:    subID,
		ResourceGroup:     rGroup,
	}
	c := NewController(controllerOpts)
	if err := c.GetAzureClients(); err != nil {
		klog.Fatal("Failed to get controller clients:", err)
	}
	c.Run(stopCh)
}
