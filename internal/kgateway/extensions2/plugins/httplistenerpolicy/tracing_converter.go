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

	var provider *tracev3.Tracing_Http
	var err error
	if config.Provider.OpenTelemetryConfig != nil {
		provider, err = convertOTelTracingConfig(ctx, config.Provider.OpenTelemetryConfig, commoncol, krtctx, parentSrc)
		if err != nil {
			return nil, err
		}
	}

	tracingConfig := &envoy_hcm.HttpConnectionManager_Tracing{
		Provider: provider,
	}
	if config.ClientSampling != nil {
		tracingConfig.ClientSampling = &typev3.Percent{
			Value: intToPercent(*config.ClientSampling),
		}
	}
	if config.MaxPathTagLength != nil {
		tracingConfig.MaxPathTagLength = &wrapperspb.UInt32Value{
			Value: *config.MaxPathTagLength,
		}
	}
	if config.OverallSampling != nil {
		tracingConfig.OverallSampling = &typev3.Percent{
			Value: intToPercent(*config.OverallSampling),
		}
	}
	if config.RandomSampling != nil {
		tracingConfig.RandomSampling = &typev3.Percent{
			Value: intToPercent(*config.RandomSampling),
		}
	}
	if config.SpawnUpstreamSpan != nil {
		tracingConfig.SpawnUpstreamSpan = &wrapperspb.BoolValue{
			Value: *config.SpawnUpstreamSpan,
		}
	}
	if config.Verbose != nil {
		tracingConfig.Verbose = *config.Verbose
	}

	return tracingConfig, nil
}

func intToPercent(in uint32) float64 {
	return float64(in) / 100
}

func convertOTelTracingConfig(
	ctx context.Context,
	config *v1alpha1.OpenTelemetryTracingConfig,
	commoncol *common.CommonCollections,
	krtctx krt.HandlerContext,
	parentSrc ir.ObjectSource,
) (*tracev3.Tracing_Http, error) {
	if config == nil {
		return nil, nil
	}
	if config.GrpcService == nil || config.GrpcService.BackendRef == nil {
		return nil, fmt.Errorf("Tracing.OpenTelemetryConfig.GrpcService.BackendRef must be specified")
	}

	var backend *ir.BackendObjectIR
	var err error
	if config.GrpcService != nil && config.GrpcService.BackendRef != nil {
		backend, err = commoncol.BackendIndex.GetBackendFromRef(krtctx, parentSrc, config.GrpcService.BackendRef.BackendObjectReference)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnresolvedBackendRef, err)
		}
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
		for _, rd := range config.ResourceDetectors {
			if rd.EnvironmentResourceDetector != nil {
				detector, _ := utils.MessageToAny(&resource_detectorsv3.EnvironmentResourceDetectorConfig{})
				translatedResourceDetectors = append(translatedResourceDetectors, &corev3.TypedExtensionConfig{
					Name:        "envoy.tracers.opentelemetry.resource_detectors.environment",
					TypedConfig: detector,
				})
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

	otelCfg, _ := utils.MessageToAny(tracingCfg)
	fmt.Println("======== otelCfg", otelCfg)

	return &tracev3.Tracing_Http{
		Name: "envoy.tracers.opentelemetry",
		ConfigType: &tracev3.Tracing_Http_TypedConfig{
			TypedConfig: otelCfg,
		},
	}, nil
}
