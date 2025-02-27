//go:build unit
// +build unit

/*
Copyright 2021 The Flux authors

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
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

var pollInterval = 50 * time.Millisecond
var testTimeout = 10 * time.Second

// Update the GitRepository once created to exercise test specific behavior
type reconcileFunc func(repo *sourcev1.GitRepository)

// reconciler waits for an object to be created, then invokes a test supplied
// function to mutate that object, simulating a controller.
// Test should invoke run() to run the background reconciler task which
// polls to wait for the object to exist before applying the update function.
// Any errors from the reconciler are asserted on test completion.
type reconciler struct {
	client    client.Client
	name      types.NamespacedName
	reconcile reconcileFunc
}

// Start the background task that waits for the object to exist then applies
// the update function.
func (r *reconciler) run(t *testing.T) {
	result := make(chan error)
	go func() {
		defer close(result)
		err := wait.PollImmediate(
			pollInterval,
			testTimeout,
			r.conditionFunc)
		result <- err
	}()
	t.Cleanup(func() {
		if err := <-result; err != nil {
			t.Errorf("Failure from test reconciler: '%v':", err.Error())
		}
	})
}

// A ConditionFunction that waits for the named GitRepository to be created,
// then sets the ready condition to true.
func (r *reconciler) conditionFunc() (bool, error) {
	var repo sourcev1.GitRepository
	if err := r.client.Get(context.Background(), r.name, &repo); err != nil {
		if errors.IsNotFound(err) {
			return false, nil // Keep polling until object is created
		}
		return true, err
	}
	r.reconcile(&repo)
	err := r.client.Status().Update(context.Background(), &repo)
	return true, err
}

func TestCreateSourceGit(t *testing.T) {
	// Default command used for multiple tests
	var command = "create source git podinfo --url=https://github.com/stefanprodan/podinfo --branch=master --timeout=" + testTimeout.String()

	cases := []struct {
		name      string
		args      string
		assert    assertFunc
		reconcile reconcileFunc
	}{
		{
			"NoArgs",
			"create source git",
			assertError("name is required"),
			nil,
		}, {
			"Succeeded",
			command,
			assertGoldenFile("testdata/create_source_git/success.golden"),
			func(repo *sourcev1.GitRepository) {
				meta.SetResourceCondition(repo, meta.ReadyCondition, metav1.ConditionTrue, sourcev1.GitOperationSucceedReason, "succeeded message")
				repo.Status.Artifact = &sourcev1.Artifact{
					Path:     "some-path",
					Revision: "v1",
				}
			},
		}, {
			"Failed",
			command,
			assertError("failed message"),
			func(repo *sourcev1.GitRepository) {
				meta.SetResourceCondition(repo, meta.ReadyCondition, metav1.ConditionFalse, sourcev1.URLInvalidReason, "failed message")
			},
		}, {
			"NoArtifact",
			command,
			assertError("GitRepository source reconciliation completed but no artifact was found"),
			func(repo *sourcev1.GitRepository) {
				// Updated with no artifact
				meta.SetResourceCondition(repo, meta.ReadyCondition, metav1.ConditionTrue, sourcev1.GitOperationSucceedReason, "succeeded message")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ns := allocateNamespace("podinfo")
			setupTestNamespace(ns, t)
			if tc.reconcile != nil {
				r := reconciler{
					client:    testEnv.client,
					name:      types.NamespacedName{Namespace: ns, Name: "podinfo"},
					reconcile: tc.reconcile,
				}
				r.run(t)
			}
			cmd := cmdTestCase{
				args:   tc.args + " -n=" + ns,
				assert: tc.assert,
			}
			cmd.runTestCmd(t)
		})
	}
}
