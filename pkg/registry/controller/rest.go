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

package controller

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/validation"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"

	"code.google.com/p/go-uuid/uuid"
)

// PodLister is anything that knows how to list pods.
type PodLister interface {
	ListPods(labels.Selector) (*api.PodList, error)
}

// REST implements apiserver.RESTStorage for the replication controller service.
type REST struct {
	registry   Registry
	podLister  PodLister
	pollPeriod time.Duration
}

// NewREST returns a new apiserver.RESTStorage for the given registry and PodLister.
func NewREST(registry Registry, podLister PodLister) *REST {
	return &REST{
		registry:   registry,
		podLister:  podLister,
		pollPeriod: time.Second * 10,
	}
}

// Create registers the given ReplicationController.
func (rs *REST) Create(obj runtime.Object) (<-chan runtime.Object, error) {
	controller, ok := obj.(*api.ReplicationController)
	if !ok {
		return nil, fmt.Errorf("not a replication controller: %#v", obj)
	}
	if len(controller.ID) == 0 {
		controller.ID = uuid.NewUUID().String()
	}
	// Pod Manifest ID should be assigned by the pod API
	controller.DesiredState.PodTemplate.DesiredState.Manifest.ID = ""
	if errs := validation.ValidateReplicationController(controller); len(errs) > 0 {
		return nil, errors.NewInvalid("replicationController", controller.ID, errs)
	}

	controller.CreationTimestamp = util.Now()

	return apiserver.MakeAsync(func() (runtime.Object, error) {
		err := rs.registry.CreateController(controller)
		if err != nil {
			return nil, err
		}
		return rs.registry.GetController(controller.ID)
	}), nil
}

// Delete asynchronously deletes the ReplicationController specified by its id.
func (rs *REST) Delete(id string) (<-chan runtime.Object, error) {
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		return &api.Status{Status: api.StatusSuccess}, rs.registry.DeleteController(id)
	}), nil
}

// Get obtains the ReplicationController specified by its id.
func (rs *REST) Get(id string) (runtime.Object, error) {
	controller, err := rs.registry.GetController(id)
	if err != nil {
		return nil, err
	}
	rs.fillCurrentState(controller)
	return controller, err
}

// List obtains a list of ReplicationControllers that match selector.
func (rs *REST) List(label, field labels.Selector) (runtime.Object, error) {
	if !field.Empty() {
		return nil, fmt.Errorf("field selector not supported yet")
	}
	controllers, err := rs.registry.ListControllers()
	if err != nil {
		return nil, err
	}
	filtered := []api.ReplicationController{}
	for _, controller := range controllers.Items {
		if label.Matches(labels.Set(controller.Labels)) {
			rs.fillCurrentState(&controller)
			filtered = append(filtered, controller)
		}
	}
	controllers.Items = filtered
	return controllers, err
}

// New creates a new ReplicationController for use with Create and Update.
func (*REST) New() runtime.Object {
	return &api.ReplicationController{}
}

// Update replaces a given ReplicationController instance with an existing
// instance in storage.registry.
func (rs *REST) Update(obj runtime.Object) (<-chan runtime.Object, error) {
	controller, ok := obj.(*api.ReplicationController)
	if !ok {
		return nil, fmt.Errorf("not a replication controller: %#v", obj)
	}
	if errs := validation.ValidateReplicationController(controller); len(errs) > 0 {
		return nil, errors.NewInvalid("replicationController", controller.ID, errs)
	}
	return apiserver.MakeAsync(func() (runtime.Object, error) {
		err := rs.registry.UpdateController(controller)
		if err != nil {
			return nil, err
		}
		return rs.registry.GetController(controller.ID)
	}), nil
}

// Watch returns ReplicationController events via a watch.Interface.
// It implements apiserver.ResourceWatcher.
func (rs *REST) Watch(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error) {
	if !field.Empty() {
		return nil, fmt.Errorf("no field selector implemented for controllers")
	}
	incoming, err := rs.registry.WatchControllers(resourceVersion)
	if err != nil {
		return nil, err
	}
	// TODO(lavalamp): remove watch.Filter, which is broken. Implement consistent way of filtering.
	// TODO(lavalamp): this watch method needs a test.
	return watch.Filter(incoming, func(e watch.Event) (watch.Event, bool) {
		repController, ok := e.Object.(*api.ReplicationController)
		if !ok {
			// must be an error event-- pass it on
			return e, true
		}
		match := label.Matches(labels.Set(repController.Labels))
		if match {
			rs.fillCurrentState(repController)
		}
		return e, match
	}), nil
}

func (rs *REST) waitForController(ctrl *api.ReplicationController) (runtime.Object, error) {
	for {
		pods, err := rs.podLister.ListPods(labels.Set(ctrl.DesiredState.ReplicaSelector).AsSelector())
		if err != nil {
			return ctrl, err
		}
		if len(pods.Items) == ctrl.DesiredState.Replicas {
			break
		}
		time.Sleep(rs.pollPeriod)
	}
	return ctrl, nil
}

func (rs *REST) fillCurrentState(ctrl *api.ReplicationController) error {
	if rs.podLister == nil {
		return nil
	}
	list, err := rs.podLister.ListPods(labels.Set(ctrl.DesiredState.ReplicaSelector).AsSelector())
	if err != nil {
		return err
	}
	ctrl.CurrentState.Replicas = len(list.Items)
	return nil
}
