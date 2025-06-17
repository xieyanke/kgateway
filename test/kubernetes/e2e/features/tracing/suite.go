package tracing

import (
	"context"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	"github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests/base"
)

var _ e2e.NewSuiteFunc = NewTestingSuite

type testingSuite struct {
	*base.BaseTestingSuite
}

func NewTestingSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	return &testingSuite{
		base.NewBaseTestingSuite(ctx, testInst, setup, testCases),
	}
}

func (s *testingSuite) TestOTelTracing() {
	// make curl request to httpbin service
	s.TestInstallation.Assertions.AssertEventualCurlResponse(
		s.Ctx,
		defaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithHostHeader("www.example.com"),
			curl.WithPath("/status/200"),
			curl.WithHost(kubeutils.ServiceFQDN(proxyService.ObjectMeta)),
		},
		&matchers.HttpResponse{
			StatusCode: 200,
		},
		20*time.Second,
		2*time.Second,
	)

	bodyMatcher := matchers.ContainSubstrings([]string{
		// The service name defined in the tracing policy
		`"service.name":"my:service"`,
		`"http.url":"http://www.example.com/status/200"`,
		`"http.method":"GET"`,
		`"http.status_code":"200"`,
		`"upstream_cluster":"kube_httpbin_httpbin_8000"`,
	})
	// query the exporter to verify the trace was generated
	s.TestInstallation.Assertions.AssertEventualCurlResponse(
		s.Ctx,
		defaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithPath("/traces"),
			curl.WithHost(kubeutils.ServiceFQDN(otelCollectorDeployment.ObjectMeta)),
			curl.WithPort(8080),
		},
		&matchers.HttpResponse{
			StatusCode: 200,
			Body:       bodyMatcher,
		},
		60*time.Second,
		2*time.Second,
	)
}
