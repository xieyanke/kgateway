package routepolicy

import (
	"errors"
	"fmt"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/config/ratelimit/v3"
	routeconfv3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	ratev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ratelimit/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/istio/pkg/kube/krt"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/pluginutils"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

const (
	rateLimitFilterName     = "envoy.filters.http.ratelimit"
	rateLimitStatPrefix     = "http_rate_limit"
	defaultRateLimitTimeout = 2 * time.Second
)

// toRateLimitFilterConfig translates a RateLimitPolicy to Envoy rate limit configuration.
func toRateLimitFilterConfig(policy *v1alpha1.RateLimitPolicy, gweCollection krt.Collection[ir.GatewayExtension], kctx krt.HandlerContext, trafficpolicy *v1alpha1.TrafficPolicy) (*ratev3.RateLimit, error) {
	if policy == nil || policy.ExtensionRef == nil {
		return nil, nil
	}

	// Get the extension name
	extensionName := string(policy.ExtensionRef.Name)

	// Use the namespace from the TrafficPolicy object, with fallback to "default"
	extensionNamespace := trafficpolicy.GetNamespace()
	if extensionNamespace == "" {
		// Use "default" namespace when none is specified
		extensionNamespace = "default"
	}

	// Get the GatewayExtension referenced by the policy
	gwExt, err := pluginutils.GetGatewayExtension(
		gweCollection,
		kctx,
		extensionName,
		extensionNamespace,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get referenced GatewayExtension %s/%s: %w",
			extensionNamespace, extensionName, err)
	}

	// Verify it's the correct type
	if gwExt.Type != v1alpha1.GatewayExtensionTypeRateLimit {
		return nil, pluginutils.ErrInvalidExtensionType(v1alpha1.GatewayExtensionTypeRateLimit, gwExt.Type)
	}

	// Get the extension's spec
	extension, err := getExtensionSpec(gwExt)
	if err != nil {
		return nil, err
	}

	// Create a timeout based on the time unit if specified, otherwise use the default from the extension
	var timeout *durationpb.Duration

	// Use timeout from extension if specified
	if extension.Timeout != "" {
		duration, err := time.ParseDuration(extension.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout in GatewayExtension %s: %w", extensionName, err)
		}
		timeout = durationpb.New(duration)
	} else {
		// Default timeout if not specified in extension
		timeout = durationpb.New(defaultRateLimitTimeout)
	}

	// Domain precedence: We prioritize the policy domain to allow specifying domain per policy.
	// If policy domain is not specified, then fall back to the extension domain.
	// This ensures each policy can have its own rate limit domain when needed.
	domain := policy.Domain
	if domain == "" && extension.Domain != "" {
		domain = extension.Domain
	}

	// Construct cluster name from the backendRef
	clusterName := ""
	if extension.GrpcService != nil && extension.GrpcService.BackendRef != nil {
		// Format: outbound|<port>||<service>.<namespace>.svc.cluster.local
		clusterName = fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local",
			extension.GrpcService.BackendRef.Port,
			extension.GrpcService.BackendRef.Name,
			gwExt.Namespace)
	} else {
		return nil, fmt.Errorf("GrpcService BackendRef not specified in GatewayExtension %s/%s",
			extensionNamespace, extensionName)
	}

	// Create a rate limit configuration
	rl := &ratev3.RateLimit{
		Domain:          domain,
		Timeout:         timeout,
		FailureModeDeny: !extension.FailOpen,
		RateLimitService: &ratelimitv3.RateLimitServiceConfig{
			GrpcService: &corev3.GrpcService{
				TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
						ClusterName: clusterName,
					},
				},
			},
			TransportApiVersion: corev3.ApiVersion_V3,
		},
		Stage:                   0,                                 // Default stage
		EnableXRatelimitHeaders: ratev3.RateLimit_DRAFT_VERSION_03, // Use latest RFC draft
		RequestType:             "both",                            // Apply to both internal and external
		StatPrefix:              rateLimitStatPrefix,
	}

	return rl, nil
}

// getExtensionSpec extracts the RateLimitProvider from a GatewayExtension
func getExtensionSpec(gwExt *ir.GatewayExtension) (*v1alpha1.RateLimitProvider, error) {
	// Check if RateLimit field exists
	if gwExt.RateLimit == nil {
		return nil, fmt.Errorf("RateLimit configuration is missing in GatewayExtension")
	}

	return gwExt.RateLimit, nil
}

// createRateLimitActions translates the API descriptors to Envoy route config rate limit actions
func createRateLimitActions(descriptors []v1alpha1.RateLimitDescriptor) ([]*routeconfv3.RateLimit_Action, error) {
	if len(descriptors) == 0 {
		return nil, errors.New("at least one descriptor is required for global rate limiting")
	}

	result := make([]*routeconfv3.RateLimit_Action, 0, len(descriptors))

	for _, desc := range descriptors {
		action := &routeconfv3.RateLimit_Action{}

		// Handle different types of descriptor sources
		if desc.Value != "" {
			// Static value
			action.ActionSpecifier = &routeconfv3.RateLimit_Action_GenericKey_{
				GenericKey: &routeconfv3.RateLimit_Action_GenericKey{
					DescriptorKey:   desc.Key,
					DescriptorValue: desc.Value,
				},
			}
		} else if desc.ValueFrom != nil {
			// Dynamic value source
			if desc.ValueFrom.RemoteAddress {
				// Use remote address as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RemoteAddress_{
					RemoteAddress: &routeconfv3.RateLimit_Action_RemoteAddress{},
				}
			} else if desc.ValueFrom.Path {
				// Use request path as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RequestHeaders_{
					RequestHeaders: &routeconfv3.RateLimit_Action_RequestHeaders{
						HeaderName:    ":path",
						DescriptorKey: desc.Key,
					},
				}
			} else if desc.ValueFrom.Header != "" {
				// Use header value as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RequestHeaders_{
					RequestHeaders: &routeconfv3.RateLimit_Action_RequestHeaders{
						HeaderName:    desc.ValueFrom.Header,
						DescriptorKey: desc.Key,
					},
				}
			} else {
				return nil, fmt.Errorf("descriptor %s has no valid value source specified", desc.Key)
			}
		} else {
			return nil, fmt.Errorf("descriptor %s has no value or valueFrom specified", desc.Key)
		}

		result = append(result, action)
	}

	return result, nil
}
