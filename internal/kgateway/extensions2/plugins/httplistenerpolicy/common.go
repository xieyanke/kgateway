package httplistenerpolicy

import (
	"errors"
	"time"

	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	google_duration "github.com/golang/protobuf/ptypes/duration"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

func ToEnvoyGrpc(in *v1alpha1.CommonGrpcService, backend *ir.BackendObjectIR) (*envoycore.GrpcService, error) {
	if in == nil {
		return nil, errors.New("grpc service object cannot be nil")
	}

	envoyGrpcService := &envoycore.GrpcService_EnvoyGrpc{
		ClusterName: backend.ClusterName(),
	}
	if in.Authority != nil {
		envoyGrpcService.Authority = *in.Authority
	}
	if in.MaxReceiveMessageLength != nil {
		envoyGrpcService.MaxReceiveMessageLength = &wrapperspb.UInt32Value{
			Value: *in.MaxReceiveMessageLength,
		}
	}
	if in.SkipEnvoyHeaders != nil {
		envoyGrpcService.SkipEnvoyHeaders = *in.SkipEnvoyHeaders
	}
	grpcService := &envoycore.GrpcService{
		TargetSpecifier: &envoycore.GrpcService_EnvoyGrpc_{
			EnvoyGrpc: &envoycore.GrpcService_EnvoyGrpc{
				ClusterName: backend.ClusterName(),
			},
		},
	}

	if in.Timeout != nil {
		grpcService.Timeout = DurationToProto(in.Timeout.Duration)
	}
	if in.InitialMetadata != nil {
		grpcService.InitialMetadata = make([]*envoycore.HeaderValue, len(in.InitialMetadata))
		for _, metadata := range in.InitialMetadata {
			grpcService.InitialMetadata = append(grpcService.GetInitialMetadata(), &envoycore.HeaderValue{
				Key:   metadata.Key,
				Value: metadata.Value,
			})
		}
	}
	if in.RetryPolicy != nil {
		retryPolicy := &envoycore.RetryPolicy{}
		if in.RetryPolicy.NumRetries != nil {
			retryPolicy.NumRetries = &wrapperspb.UInt32Value{
				Value: *in.RetryPolicy.NumRetries,
			}
		}
		if in.RetryPolicy.RetryBackOff != nil {
			retryPolicy.RetryBackOff = &envoycore.BackoffStrategy{
				BaseInterval: DurationToProto(in.RetryPolicy.RetryBackOff.BaseInterval.Duration),
			}
			if in.RetryPolicy.RetryBackOff.MaxInterval != nil {
				if in.RetryPolicy.RetryBackOff.MaxInterval.Duration.Nanoseconds() < in.RetryPolicy.RetryBackOff.BaseInterval.Duration.Nanoseconds() {
					// error out
					panic("error in base and max interval")
				} else {
					retryPolicy.GetRetryBackOff().MaxInterval = DurationToProto(in.RetryPolicy.RetryBackOff.MaxInterval.Duration)
				}
			}
		}
		grpcService.RetryPolicy = retryPolicy
	}
	return grpcService, nil

}

// DurationToProto converts a go Duration to a protobuf Duration.
func DurationToProto(d time.Duration) *google_duration.Duration {
	return &google_duration.Duration{
		Seconds: int64(d) / int64(time.Second),
		Nanos:   int32(int64(d) % int64(time.Second)),
	}
}
