package acesslog

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	"github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	testdefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
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

// SetupSuite runs before all tests in the suite
func (s *testingSuite) SetupSuite() {
	s.BaseTestingSuite.SetupSuite()

	s.TestInstallation.Assertions.EventuallyHTTPRouteCondition(s.Ctx, "httpbin", "httpbin", gwv1.RouteConditionAccepted, metav1.ConditionTrue)
}

// TestAccessLogWithFileSink tests access log with file sink
func (s *testingSuite) TestAccessLogWithFileSink() {
	pods := s.getPods(fmt.Sprintf("app.kubernetes.io/name=%s", gatewayObjectMeta.GetName()))
	s.sendTestRequest()

	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		logs, err := s.TestInstallation.Actions.Kubectl().GetContainerLogs(s.Ctx, gatewayService.ObjectMeta.GetNamespace(), pods[0])
		s.Require().NoError(err)

		// Verify the log contains the expected JSON pattern
		assert.Contains(c, logs, `"authority":"www.example.com"`)
		assert.Contains(c, logs, `"method":"GET"`)
		assert.Contains(c, logs, `"path":"/status/200"`)
		assert.Contains(c, logs, `"protocol":"HTTP/1.1"`)
		assert.Contains(c, logs, `"response_code":200`)
		assert.Contains(c, logs, `"backendCluster":"kube_httpbin_httpbin_8000"`)
	}, 5*time.Second, 100*time.Millisecond)
}

// TestAccessLogWithGrpcSink tests access log with grpc sink
func (s *testingSuite) TestAccessLogWithGrpcSink() {
	pods := s.getPods("kgateway=gateway-proxy-access-logger")
	s.sendTestRequest()

	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		logs, err := s.TestInstallation.Actions.Kubectl().GetContainerLogs(s.Ctx, accessLoggerDeployment.ObjectMeta.GetNamespace(), pods[0])
		s.Require().NoError(err)

		// Verify the log contains the expected JSON pattern
		assert.Contains(c, logs, `"logger_name":"test-accesslog-service"`)
		assert.Contains(c, logs, `"cluster":"kube_httpbin_httpbin_8000"`)
	}, 5*time.Second, 100*time.Millisecond)
}

// TestAccessLogWithOTelSink tests access log with OTel sink
func (s *testingSuite) TestAccessLogWithOTelSink() {
	s.sendTestRequest()

	bodyMatcher := matchers.ContainSubstrings([]string{
		`"log_name":"test-otel-accesslog-service"`,
		`"body":"\"GET /status/200 200 \"www.example.com\" \"kube_httpbin_httpbin_8000\"\\n'"`,
	})
	// query the exporter to verify the trace was generated
	s.TestInstallation.Assertions.AssertEventualCurlResponse(
		s.Ctx,
		defaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithPath("/logs"),
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

func (s *testingSuite) sendTestRequest() {
	s.TestInstallation.Assertions.AssertEventualCurlResponse(
		s.Ctx,
		testdefaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithHost(kubeutils.ServiceFQDN(gatewayService.ObjectMeta)),
			curl.VerboseOutput(),
			curl.WithHostHeader("www.example.com"),
			curl.WithPath("/status/200"),
			curl.WithPort(8080),
		},
		&matchers.HttpResponse{
			StatusCode: http.StatusOK,
		},
	)
}

func (s *testingSuite) getPods(label string) []string {
	s.TestInstallation.Assertions.EventuallyPodsRunning(
		s.Ctx,
		accessLoggerDeployment.ObjectMeta.GetNamespace(),
		metav1.ListOptions{
			LabelSelector: label,
		},
	)
	// check access log
	pods, err := s.TestInstallation.Actions.Kubectl().GetPodsInNsWithLabel(
		s.Ctx,
		accessLoggerDeployment.ObjectMeta.GetNamespace(),
		label,
	)
	s.Require().NoError(err)
	s.Require().Len(pods, 1)

	return pods
}
