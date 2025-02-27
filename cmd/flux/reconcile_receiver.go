/*
Copyright 2020 The Flux authors

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
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	notificationv1 "github.com/fluxcd/notification-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"

	"github.com/fluxcd/flux2/internal/utils"
)

var reconcileReceiverCmd = &cobra.Command{
	Use:   "receiver [name]",
	Short: "Reconcile a Receiver",
	Long:  `The reconcile receiver command triggers a reconciliation of a Receiver resource and waits for it to finish.`,
	Example: `  # Trigger a reconciliation for an existing receiver
  flux reconcile receiver main`,
	ValidArgsFunction: resourceNamesCompletionFunc(notificationv1.GroupVersion.WithKind(notificationv1.ReceiverKind)),
	RunE:              reconcileReceiverCmdRun,
}

func init() {
	reconcileCmd.AddCommand(reconcileReceiverCmd)
}

func reconcileReceiverCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("receiver name is required")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(kubeconfigArgs)
	if err != nil {
		return err
	}

	namespacedName := types.NamespacedName{
		Namespace: *kubeconfigArgs.Namespace,
		Name:      name,
	}

	var receiver notificationv1.Receiver
	err = kubeClient.Get(ctx, namespacedName, &receiver)
	if err != nil {
		return err
	}

	if receiver.Spec.Suspend {
		return fmt.Errorf("resource is suspended")
	}

	logger.Actionf("annotating Receiver %s in %s namespace", name, *kubeconfigArgs.Namespace)
	if receiver.Annotations == nil {
		receiver.Annotations = map[string]string{
			meta.ReconcileRequestAnnotation: time.Now().Format(time.RFC3339Nano),
		}
	} else {
		receiver.Annotations[meta.ReconcileRequestAnnotation] = time.Now().Format(time.RFC3339Nano)
	}
	if err := kubeClient.Update(ctx, &receiver); err != nil {
		return err
	}
	logger.Successf("Receiver annotated")

	logger.Waitingf("waiting for Receiver reconciliation")
	if err := wait.PollImmediate(rootArgs.pollInterval, rootArgs.timeout,
		isReceiverReady(ctx, kubeClient, namespacedName, &receiver)); err != nil {
		return err
	}

	logger.Successf("Receiver reconciliation completed")

	return nil
}
