package httplistenerpolicy

import (
	"context"
	"fmt"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracev3 "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	envoy_hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	resource_detectorsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"
	samplersv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/samplers/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"istio.io/istio/pkg/kube/krt"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/common"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils"
)

func convertTracingConfig(
	ctx context.Context,
	policy *v1alpha1.HTTPListenerPolicy,
	commoncol *common.CommonCollections,
	krtctx krt.HandlerContext,
	parentSrc ir.ObjectSource,
) (*envoy_hcm.HttpConnectionManager_Tracing, error) {
	config := policy.Spec.Tracing
	if config == nil || config.Provider == nil {
		return nil, nil
	}

	if config.Provider.OpenTelemetry == nil || config.Provider.OpenTelemetry.GrpcService == nil || config.Provider.OpenTelemetry.GrpcService.BackendRef == nil {
		return nil, fmt.Errorf("Tracing.OpenTelemetryConfig.GrpcService.BackendRef must be specified")
	}

	backend, err := commoncol.BackendIndex.GetBackendFromRef(krtctx, parentSrc, config.Provider.OpenTelemetry.GrpcService.BackendRef.BackendObjectReference)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnresolvedBackendRef, err)
	}

	return translateTracing(config, backend)
}

func translateTracing(
	config *v1alpha1.Tracing,
	backend *ir.BackendObjectIR,
) (*envoy_hcm.HttpConnectionManager_Tracing, error) {
	if config == nil || config.Provider == nil {
		return nil, nil
	}

	if config.Provider.OpenTelemetry == nil || config.Provider.OpenTelemetry.GrpcService == nil || config.Provider.OpenTelemetry.GrpcService.BackendRef == nil {
		return nil, fmt.Errorf("Tracing.OpenTelemetryConfig.GrpcService.BackendRef must be specified")
	}

	provider, err := convertOTelTracingConfig(config.Provider.OpenTelemetry, backend)
	if err != nil {
		return nil, err
	}

	tracingConfig := &envoy_hcm.HttpConnectionManager_Tracing{
		Provider: provider,
	}
	if config.ClientSampling != nil {
		tracingConfig.ClientSampling = &typev3.Percent{
			Value: float64(*config.ClientSampling),
		}
	}
	if config.RandomSampling != nil {
		tracingConfig.RandomSampling = &typev3.Percent{
			Value: float64(*config.RandomSampling),
		}
	}
	if config.OverallSampling != nil {
		tracingConfig.OverallSampling = &typev3.Percent{
			Value: float64(*config.OverallSampling),
		}
	}
	if config.Verbose != nil {
		tracingConfig.Verbose = *config.Verbose
	}
	if config.MaxPathTagLength != nil {
		tracingConfig.MaxPathTagLength = &wrapperspb.UInt32Value{
			Value: *config.MaxPathTagLength,
		}
	}
	// TODO: Custom tags
	if config.SpawnUpstreamSpan != nil {
		tracingConfig.SpawnUpstreamSpan = &wrapperspb.BoolValue{
			Value: *config.SpawnUpstreamSpan,
		}
	}

	return tracingConfig, nil
}

func convertOTelTracingConfig(
	config *v1alpha1.OpenTelemetryTracingConfig,
	backend *ir.BackendObjectIR,
) (*tracev3.Tracing_Http, error) {
	if config == nil {
		return nil, nil
	}

	envoyGrpcService, err := ToEnvoyGrpc(config.GrpcService, backend)
	if err != nil {
		return nil, err
	}

	tracingCfg := &tracev3.OpenTelemetryConfig{
		GrpcService: envoyGrpcService,
		ServiceName: config.ServiceName,
	}

	if len(config.ResourceDetectors) != 0 {
		translatedResourceDetectors := make([]*corev3.TypedExtensionConfig, len(config.ResourceDetectors))
		for i, rd := range config.ResourceDetectors {
			if rd.EnvironmentResourceDetector != nil {
				detector, _ := utils.MessageToAny(&resource_detectorsv3.EnvironmentResourceDetectorConfig{})
				translatedResourceDetectors[i] = &corev3.TypedExtensionConfig{
					Name:        "envoy.tracers.opentelemetry.resource_detectors.environment",
					TypedConfig: detector,
				}
			}
		}
		tracingCfg.ResourceDetectors = translatedResourceDetectors
	}

	if config.Sampler != nil {
		if config.Sampler.AlwaysOn != nil {
			alwaysOnSampler, _ := utils.MessageToAny(&samplersv3.AlwaysOnSamplerConfig{})
			tracingCfg.Sampler = &corev3.TypedExtensionConfig{
				Name:        "envoy.tracers.opentelemetry.samplers.always_on",
				TypedConfig: alwaysOnSampler,
			}
		}
	}

	otelCfg, err := utils.MessageToAny(tracingCfg)
	if err != nil {
		return nil, err
	}

	return &tracev3.Tracing_Http{
		Name: "envoy.tracers.opentelemetry",
		ConfigType: &tracev3.Tracing_Http_TypedConfig{
			TypedConfig: otelCfg,
		},
	}, nil
}
