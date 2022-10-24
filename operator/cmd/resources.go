// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package cmd

import (
	core_v1 "k8s.io/api/core/v1"

	"github.com/cilium/cilium/pkg/hive"
	"github.com/cilium/cilium/pkg/hive/cell"
	cilium_api_v2alpha1 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2alpha1"
	k8sClient "github.com/cilium/cilium/pkg/k8s/client"
	"github.com/cilium/cilium/pkg/k8s/resource"
	"github.com/cilium/cilium/pkg/k8s/utils"
)

var resourcesCell = cell.Module(
	"resources",
	cell.Provide(
		func(lc hive.Lifecycle, c k8sClient.Clientset) resource.Resource[*core_v1.Service] {
			return resource.New[*core_v1.Service](
				lc,
				utils.ListerWatcherFromTyped[*core_v1.ServiceList](c.CoreV1().Services("")),
			)
		},
		func(lc hive.Lifecycle, c k8sClient.Clientset) resource.Resource[*cilium_api_v2alpha1.CiliumLoadBalancerIPPool] {
			return resource.New[*cilium_api_v2alpha1.CiliumLoadBalancerIPPool](
				lc,
				utils.ListerWatcherFromTyped[*cilium_api_v2alpha1.CiliumLoadBalancerIPPoolList](c.CiliumV2alpha1().CiliumLoadBalancerIPPools()),
			)
		},
	),
)
