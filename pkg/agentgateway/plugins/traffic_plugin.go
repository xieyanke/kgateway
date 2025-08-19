package plugins

import (
	"fmt"

	"github.com/agentgateway/agentgateway/go/api"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
	"github.com/kgateway-dev/kgateway/v2/pkg/logging"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
)

const (
	trafficPluginName = "traffic-policy-plugin"

	extauthPolicySuffix = ":extauth"
	rbacPolicySuffix    = ":rbac"
)

// TrafficPlugin converts a TrafficPolicy to an agentgateway policy
type TrafficPlugin struct{}

// NewTrafficPlugin creates a new TrafficPolicy plugin
func NewTrafficPlugin() *TrafficPlugin {
	return &TrafficPlugin{}
}

// GroupKind returns the GroupKind of the policy this plugin handles
func (p *TrafficPlugin) GroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: wellknown.TrafficPolicyGVK.GroupKind().Group,
		Kind:  wellknown.TrafficPolicyGVK.GroupKind().Kind,
	}
}

// Name returns the name of this plugin
func (p *TrafficPlugin) Name() string {
	return trafficPluginName
}

// GeneratePolicies generates agentgateway policies from TrafficPolicy resources
func (p *TrafficPlugin) GeneratePolicies(ctx krt.HandlerContext, agw *AgwCollections) ([]ADPPolicy, error) {
	logger := logging.New("agentgateway/plugins/traffic")

	trafficPolicies := agw.TrafficPolicies
	if trafficPolicies == nil {
		logger.Debug("traffic policies collection is nil, skipping traffic policy generation")
		return nil, nil
	}

	return p.GenerateTrafficPolicies(ctx, trafficPolicies, agw.Backends, agw.GatewayExtensions)
}

// GenerateTrafficPolicies generates policies for traffic policies
func (p *TrafficPlugin) GenerateTrafficPolicies(ctx krt.HandlerContext,
	trafficPolicies krt.Collection[*v1alpha1.TrafficPolicy],
	backends krt.Collection[*v1alpha1.Backend],
	gatewayExtensions krt.Collection[*v1alpha1.GatewayExtension]) ([]ADPPolicy, error) {
	logger := logging.New("agentgateway/plugins/traffic")
	logger.Debug("generating traffic policies")

	var trafficPoliciesResult []ADPPolicy

	// Fetch all traffic policies and process them
	allTrafficPolicies := krt.Fetch(ctx, trafficPolicies)

	for _, trafficPolicy := range allTrafficPolicies {
		policies := p.generatePoliciesForTrafficPolicy(ctx, gatewayExtensions, backends, trafficPolicy)
		trafficPoliciesResult = append(trafficPoliciesResult, policies...)
	}

	logger.Debug("generated traffic policies", "count", len(trafficPoliciesResult))
	return trafficPoliciesResult, nil
}

// generatePoliciesForTrafficPolicy generates policies for a single traffic policy
func (p *TrafficPlugin) generatePoliciesForTrafficPolicy(ctx krt.HandlerContext,
	gatewayExtensions krt.Collection[*v1alpha1.GatewayExtension],
	backends krt.Collection[*v1alpha1.Backend],
	trafficPolicy *v1alpha1.TrafficPolicy) []ADPPolicy {
	logger := logging.New("agentgateway/plugins/traffic")
	var adpPolicies []ADPPolicy

	isMcpTarget := false
	for _, target := range trafficPolicy.Spec.TargetRefs {
		var policyTarget *api.PolicyTarget

		switch string(target.Kind) {
		case wellknown.GatewayKind:
			policyTarget = &api.PolicyTarget{
				Kind: &api.PolicyTarget_Gateway{
					Gateway: trafficPolicy.Namespace + "/" + string(target.Name),
				},
			}
			// TODO(npolshak): add listener support once https://github.com/agentgateway/agentgateway/pull/323 goes in
			//if target.SectionName != nil {
			//	policyTarget = &api.PolicyTarget{
			//		Kind: &api.PolicyTarget_Listener{
			//			Listener: InternalGatewayName(trafficPolicy.Namespace, string(target.Name), string(*target.SectionName)),
			//		},
			//	}
			//}

		case wellknown.HTTPRouteKind:
			policyTarget = &api.PolicyTarget{
				Kind: &api.PolicyTarget_Route{
					Route: trafficPolicy.Namespace + "/" + string(target.Name),
				},
			}
			// TODO(npolshak): add route rule support once https://github.com/agentgateway/agentgateway/pull/323 goes in
			//if target.SectionName != nil {
			//	policyTarget = &api.PolicyTarget{
			//		Kind: &api.PolicyTarget_RouteRule{
			//			RouteRule: trafficPolicy.Namespace + "/" + string(target.Name) + "/" + string(*target.SectionName),
			//		},
			//	}
			//}

		case wellknown.BackendGVK.Kind:
			// kgateway backend kind (MCP, AI, etc.)

			// Look up the Backend referenced by the policy
			backendKey := fmt.Sprintf("%s/%s", trafficPolicy.Namespace, target.Name)
			backend := krt.FetchOne(ctx, backends, krt.FilterKey(backendKey))
			if backend == nil {
				logger.Error("backend not found", "name", target.Name, "namespace", trafficPolicy.Namespace)
				return nil
			}
			backendSpec := (*backend).Spec
			if backendSpec.Type == v1alpha1.BackendTypeMCP {
				isMcpTarget = true
			}
			policyTarget = &api.PolicyTarget{
				Kind: &api.PolicyTarget_Backend{
					Backend: trafficPolicy.Namespace + "/" + string(target.Name),
				},
			}
		default:
			logger.Warn("unsupported target kind", "kind", target.Kind, "policy", trafficPolicy.Name)
			continue
		}

		if policyTarget != nil {
			translatedPolicies := p.translateTrafficPolicyToADP(ctx, gatewayExtensions, trafficPolicy, string(target.Name), policyTarget, isMcpTarget)
			adpPolicies = append(adpPolicies, translatedPolicies...)
		}
	}

	return adpPolicies
}

// translateTrafficPolicyToADP converts a TrafficPolicy to ADP Policy resources
func (p *TrafficPlugin) translateTrafficPolicyToADP(
	ctx krt.HandlerContext,
	gatewayExtensions krt.Collection[*v1alpha1.GatewayExtension],
	trafficPolicy *v1alpha1.TrafficPolicy,
	policyTargetName string,
	policyTarget *api.PolicyTarget,
	isMcpTarget bool,
) []ADPPolicy {
	adpPolicies := make([]ADPPolicy, 0)

	// Generate a base policy name from the TrafficPolicy reference
	policyName := fmt.Sprintf("trafficpolicy/%s/%s/%s", trafficPolicy.Namespace, trafficPolicy.Name, policyTargetName)

	// Convert ExtAuth policy if present
	if trafficPolicy.Spec.ExtAuth != nil && trafficPolicy.Spec.ExtAuth.ExtensionRef != nil {
		extAuthPolicies := p.processExtAuthPolicy(ctx, gatewayExtensions, trafficPolicy, policyName, policyTarget)
		adpPolicies = append(adpPolicies, extAuthPolicies...)
	}

	// Conver RBAC policy if present
	if trafficPolicy.Spec.RBAC != nil {
		rbacPolicies := p.processRBACPolicy(trafficPolicy, policyName, policyTarget, isMcpTarget)
		adpPolicies = append(adpPolicies, rbacPolicies...)
	}

	// TODO: Add support for other policy types as needed:
	// - RateLimit
	// - Transformation
	// - ExtProc
	// - AI policies
	// etc.

	return adpPolicies
}

// processExtAuthPolicy processes ExtAuth configuration and creates corresponding ADP policies
func (p *TrafficPlugin) processExtAuthPolicy(ctx krt.HandlerContext, gatewayExtensions krt.Collection[*v1alpha1.GatewayExtension], trafficPolicy *v1alpha1.TrafficPolicy, policyName string, policyTarget *api.PolicyTarget) []ADPPolicy {
	logger := logging.New("agentgateway/plugins/traffic")

	// Look up the GatewayExtension referenced by the ExtAuth policy
	extensionName := trafficPolicy.Spec.ExtAuth.ExtensionRef.Name
	gwExtKey := fmt.Sprintf("%s/%s", trafficPolicy.Namespace, extensionName)
	gwExt := krt.FetchOne(ctx, gatewayExtensions, krt.FilterKey(gwExtKey))

	if gwExt == nil || (*gwExt).Spec.Type != v1alpha1.GatewayExtensionTypeExtAuth || (*gwExt).Spec.ExtAuth == nil {
		logger.Error("gateway extension not found or not of type ExtAuth", "extension", gwExtKey)
		return nil
	}
	extAuth := (*gwExt).Spec.ExtAuth

	// Extract service target from GatewayExtension's ExtAuth configuration
	var extauthSvcTarget *api.BackendReference
	if extAuth.GrpcService != nil && extAuth.GrpcService.BackendRef != nil {
		backendRef := extAuth.GrpcService.BackendRef
		serviceName := string(backendRef.Name)
		port := uint32(80) // default port
		if backendRef.Port != nil {
			port = uint32(*backendRef.Port)
		}
		// use trafficPolicy namespace as default
		namespace := trafficPolicy.Namespace
		if backendRef.Namespace != nil {
			namespace = string(*backendRef.Namespace)
		}
		serviceHost := kubeutils.ServiceFQDN(metav1.ObjectMeta{Namespace: namespace, Name: serviceName})
		extauthSvcTarget = &api.BackendReference{
			Kind: &api.BackendReference_Service{Service: namespace + "/" + serviceHost},
			Port: port,
		}
	}

	if extauthSvcTarget == nil {
		logger.Warn("failed to translate traffic policy", "policy", trafficPolicy.Name, "target", policyTarget, "error", "missing extauthservice target")
		return nil
	}

	extauthPolicy := &api.Policy{
		Name:   policyName + extauthPolicySuffix,
		Target: policyTarget,
		Spec: &api.PolicySpec{
			Kind: &api.PolicySpec_ExtAuthz{
				ExtAuthz: &api.PolicySpec_ExternalAuth{
					Target:  extauthSvcTarget,
					Context: trafficPolicy.Spec.ExtAuth.ContextExtensions,
				},
			},
		},
	}

	logger.Debug("generated ExtAuth policy",
		"policy", trafficPolicy.Name,
		"agentgateway_policy", extauthPolicy.Name,
		"target", extauthSvcTarget)

	return []ADPPolicy{{Policy: extauthPolicy}}
}

// processRBACPolicy processes RBAC configuration and creates corresponding ADP policies
func (p *TrafficPlugin) processRBACPolicy(
	trafficPolicy *v1alpha1.TrafficPolicy,
	policyName string,
	policyTarget *api.PolicyTarget,
	isMCP bool,
) []ADPPolicy {
	logger := logging.New("agentgateway/plugins/traffic/rbac")

	var allowPolicies, denyPolicies []string
	if trafficPolicy.Spec.RBAC.Action == v1alpha1.AuthorizationPolicyActionDeny {
		denyPolicies = append(denyPolicies, trafficPolicy.Spec.RBAC.Policy.MatchExpressions...)
	} else {
		allowPolicies = append(allowPolicies, trafficPolicy.Spec.RBAC.Policy.MatchExpressions...)
	}

	var rbacPolicy *api.Policy
	if isMCP {
		rbacPolicy = &api.Policy{
			Name:   policyName + rbacPolicySuffix,
			Target: policyTarget,
			Spec: &api.PolicySpec{
				Kind: &api.PolicySpec_McpAuthorization{
					McpAuthorization: &api.PolicySpec_RBAC{
						Allow: allowPolicies,
						Deny:  denyPolicies,
					},
				},
			},
		}
	} else {
		rbacPolicy = &api.Policy{
			Name:   policyName + rbacPolicySuffix,
			Target: policyTarget,
			Spec: &api.PolicySpec{
				Kind: &api.PolicySpec_Authorization{
					Authorization: &api.PolicySpec_RBAC{
						Allow: allowPolicies,
						Deny:  denyPolicies,
					},
				},
			},
		}
	}

	logger.Debug("generated RBAC policy",
		"policy", trafficPolicy.Name,
		"agentgateway_policy", rbacPolicy.Name,
		"target", policyTarget)

	return []ADPPolicy{{Policy: rbacPolicy}}
}

// Verify that TrafficPlugin implements the required interfaces
var _ PolicyPlugin = (*TrafficPlugin)(nil)
