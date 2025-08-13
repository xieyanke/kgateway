package trafficpolicy

import (
	"context"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/pkg/settings"
	"github.com/kgateway-dev/kgateway/v2/pkg/validator"
	"github.com/kgateway-dev/kgateway/v2/pkg/xds/bootstrap"
)

// validateWithRouteReplacementMode performs validation based on route replacement mode.
// Callers who need route replacement mode behavior should use this method instead of calling
// the Validate() method on the TrafficPolicy type directly.
func validateWithRouteReplacementMode(ctx context.Context, p *TrafficPolicy, v validator.Validator, mode settings.RouteReplacementMode) error {
	switch mode {
	case settings.RouteReplacementStandard:
		return p.Validate()
	case settings.RouteReplacementStrict:
		if err := p.Validate(); err != nil {
			return err
		}
		return validateXDS(ctx, p, v)
	}
	return nil
}

// validateXDS performs only xDS validation by building a partial bootstrap config and validating
// it via envoy validate mode. It re-uses the ApplyForRoute method to ensure that the translation
// and validation logic go through the same code path as normal.
// This method can be called independently when only xDS validation is needed.
func validateXDS(ctx context.Context, p *TrafficPolicy, v validator.Validator) error {
	// use a fake translation pass to ensure we have the desired typed filter config
	// on the placeholder vhost.
	typedPerFilterConfig := ir.TypedFilterConfigMap(map[string]proto.Message{})
	fakePass := NewGatewayTranslationPass(ctx, ir.GwTranslationCtx{}, nil)
	if err := fakePass.ApplyForRoute(ctx, &ir.RouteContext{
		Policy:            p,
		TypedFilterConfig: typedPerFilterConfig,
	}, nil); err != nil {
		return err
	}

	// build a partial bootstrap config with the typed filter config applied.
	builder := bootstrap.New()
	for name, config := range typedPerFilterConfig {
		builder.AddFilterConfig(name, config)
	}
	bootstrap, err := builder.Build()
	if err != nil {
		return err
	}
	data, err := protojson.Marshal(bootstrap)
	if err != nil {
		return err
	}

	// shell out to envoy to validate the partial bootstrap config.
	return v.Validate(ctx, string(data))
}
