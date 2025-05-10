package backend

import (
	"context"

	envoy_config_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_dfp_cluster "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/dynamic_forward_proxy/v3"
	envoydfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_forward_proxy/v3"
	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils"
)

var (
	dfpFilterConfig = &envoydfp.FilterConfig{
		ImplementationSpecifier: &envoydfp.FilterConfig_SubClusterConfig{
			SubClusterConfig: &envoydfp.SubClusterConfig{},
		},
	}
)

func processDynamicForwardProxy(ctx context.Context, in *v1alpha1.DynamicForwardProxyBackend, out *envoy_config_cluster_v3.Cluster) error {
	out.LbPolicy = envoy_config_cluster_v3.Cluster_CLUSTER_PROVIDED
	c := &envoy_dfp_cluster.ClusterConfig{
		ClusterImplementationSpecifier: &envoy_dfp_cluster.ClusterConfig_SubClustersConfig{
			SubClustersConfig: &envoy_dfp_cluster.SubClustersConfig{
				LbPolicy: envoy_config_cluster_v3.Cluster_LEAST_REQUEST,
			},
		},
	}
	anyCluster, err := utils.MessageToAny(c)
	if err != nil {
		return err
	}

	// the upstream has a DNS name. We need Envoy to resolve the DNS name
	// set the type to strict dns
	out.ClusterDiscoveryType = &envoy_config_cluster_v3.Cluster_ClusterType{
		ClusterType: &envoy_config_cluster_v3.Cluster_CustomClusterType{
			Name:        "envoy.clusters.dynamic_forward_proxy",
			TypedConfig: anyCluster,
		},
	}

	return nil
}
