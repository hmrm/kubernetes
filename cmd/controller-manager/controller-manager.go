/*
Copyright 2014 Google Inc. All rights reserved.

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

// The controller manager is responsible for monitoring replication
// controllers, and creating corresponding pods to achieve the desired
// state.  It uses the API to listen for new controllers and to create/delete
// pods.
package main

import (
	"flag"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/latest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/controller"
	_ "github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	masterPkg "github.com/GoogleCloudPlatform/kubernetes/pkg/master"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version/verflag"
	"github.com/golang/glog"
)

var (
	master  = flag.String("master", "", "The address of the Kubernetes API server")
	port    = flag.Int("port", masterPkg.ControllerManagerPort, "The port that the controller-manager's http service runs on")
	address = flag.String("address", "127.0.0.1", "The address to serve from")
)

func main() {
	flag.Parse()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	if len(*master) == 0 {
		glog.Fatal("usage: controller-manager -master <master>")
	}

	kubeClient, err := client.New(*master, latest.OldestVersion, nil)
	if err != nil {
		glog.Fatalf("Invalid -master: %v", err)
	}

	go http.ListenAndServe(net.JoinHostPort(*address, strconv.Itoa(*port)), nil)

	controllerManager := controller.NewReplicationManager(kubeClient)
	controllerManager.Run(10 * time.Second)
	select {}
}
