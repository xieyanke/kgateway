package leaderelection

import (
	"context"
	"strings"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

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

func (s *testingSuite) TestLeaderWritesStatus() {
	s.waitUntilStartsLeading()
	leader := s.getLeader()

	// Scale the deployment to 2 replicas so the other can take over when the leader is killed
	err := s.TestInstallation.Actions.Kubectl().Scale(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayDeployment, 2)
	s.NoError(err)
	defer func() {
		err = s.TestInstallation.Actions.Kubectl().Scale(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayDeployment, 1)
		s.NoError(err)
	}()

	// Verify that the other pod is the follower
	s.getFollower()

	// Kill the leader. Translation should still occur but status should not be written while a new leader is elected.
	s.killLeader(leader)

	// Since the route does not exist, it should return a 404
	s.assertCurlResponseCode(404)

	// Create a route. The following should happen in order :
	// - It should be translated by the follower
	// - It should not have a status set since the leader is deleted but the lease has not expired
	// - Once a leader is elected, it should be accepted
	err = s.TestInstallation.Actions.Kubectl().ApplyFile(s.Ctx, routeManifest)
	s.NoError(err)
	defer func() {
		err = s.TestInstallation.Actions.Kubectl().DeleteFile(s.Ctx, routeManifest)
		s.NoError(err)
	}()

	s.assertCurlResponseCode(200)
	s.assertRouteHasNoStatus()
	s.TestInstallation.Assertions.EventuallyHTTPRouteCondition(s.Ctx, "httpbin", "httpbin", gwv1.RouteConditionAccepted, metav1.ConditionTrue)

	// Verify that a new leader was elected
	s.NotNil(s.getLeader(), "no leader found")
}

func (s *testingSuite) TestLeaderDeploysProxy() {
	s.waitUntilStartsLeading()
	leader := s.getLeader()

	// Scale the deployment to 2 replicas so the other can take over when the leader is killed
	err := s.TestInstallation.Actions.Kubectl().Scale(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayDeployment, 2)
	s.NoError(err)
	defer func() {
		err = s.TestInstallation.Actions.Kubectl().Scale(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayDeployment, 1)
		s.NoError(err)
	}()

	// Verify that the other pod is the follower
	s.getFollower()

	// Kill the leader. When a gateway is created, it should not be deployed until a new leader is elected.
	s.killLeader(leader)

	// Create a gateway. It should not be deployed until a new leader is elected
	err = s.TestInstallation.Actions.Kubectl().ApplyFile(s.Ctx, gatewayManifest)
	s.NoError(err)
	defer func() {
		err = s.TestInstallation.Actions.Kubectl().DeleteFile(s.Ctx, gatewayManifest)
		s.NoError(err)
	}()

	begin := time.Now()
	s.TestInstallation.Assertions.EventuallyObjectsExist(s.Ctx, proxyDeployment, proxyService)
	diff := time.Now().Sub(begin).Seconds()

	// The time to deploy the proxy is greater than the lease renewal period.
	s.Greater(diff, 10.0)

	// Verify that a new leader was elected
	s.NotNil(s.getLeader(), "no leader found")
}

func (s *testingSuite) waitUntilStartsLeading() {
	// Initially sleep as the new deployment might be rolling out
	time.Sleep(10 * time.Second)

	s.TestInstallation.Assertions.Gomega.Eventually(func(g gomega.Gomega) {
		out, err := s.TestInstallation.Actions.Kubectl().GetContainerLogs(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayDeployment)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to get pod logs")
		g.Expect(out).To(gomega.ContainSubstring("starting leadership"))
	}, "30s", "10s").Should(gomega.Succeed())
}

func (s *testingSuite) getLeader() string {
	pods, err := s.TestInstallation.Actions.Kubectl().GetPodsInNsWithLabel(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayPodLabel)
	s.NoError(err)
	return s.getLeaderFromPods(pods...)
}

func (s *testingSuite) getLeaderFromPods(pods ...string) string {
	var leader string
	s.TestInstallation.Assertions.Gomega.Eventually(func(g gomega.Gomega) {
		for _, pod := range pods {
			out, err := s.TestInstallation.Actions.Kubectl().GetContainerLogs(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, pod)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to get pod logs")
			if strings.Contains(out, "starting leadership") {
				leader = pod
			}
			g.Expect(leader).ToNot(gomega.BeNil())
		}
	}, "30s", "10s").Should(gomega.Succeed())
	return leader
}

func (s *testingSuite) getFollower() string {
	pods, err := s.TestInstallation.Actions.Kubectl().GetPodsInNsWithLabel(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, defaults.KGatewayPodLabel)
	s.NoError(err)
	return s.getFollowerFromPods(pods...)
}

func (s *testingSuite) getFollowerFromPods(pods ...string) string {
	var follower string
	s.TestInstallation.Assertions.Gomega.Eventually(func(g gomega.Gomega) {
		for _, pod := range pods {
			out, err := s.TestInstallation.Actions.Kubectl().GetContainerLogs(s.Ctx, s.TestInstallation.Metadata.InstallNamespace, pod)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to get pod logs")
			g.Expect(out).To(gomega.ContainSubstring("new leader elected"))
			if !strings.Contains(out, "starting leadership") {
				follower = pod
			}
			g.Expect(follower).ToNot(gomega.BeNil())
		}
	}, "30s", "10s").Should(gomega.Succeed())
	return follower
}

func (s *testingSuite) killLeader(leader string) {
	// Kill the leader so another pod can assume leadership
	_, _, err := s.TestInstallation.Actions.Kubectl().Execute(s.Ctx, "delete", "pod", "-n", s.TestInstallation.Metadata.InstallNamespace, leader)
	s.NoError(err)
	s.TestInstallation.Assertions.Gomega.Eventually(func(g gomega.Gomega) {
		_, _, err := s.TestInstallation.Actions.Kubectl().Execute(s.Ctx, "get", "pod", "-n", s.TestInstallation.Metadata.InstallNamespace, leader)
		g.Expect(err).To(gomega.HaveOccurred(), "Failed to delete leader")
	}, "120s", "1s").Should(gomega.Succeed())
}

func (s *testingSuite) assertCurlResponseCode(code int) {
	s.TestInstallation.Assertions.AssertEventualCurlResponse(
		s.Ctx,
		defaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithHostHeader("www.example.com"),
			curl.WithPath("/status/200"),
			curl.WithHost(kubeutils.ServiceFQDN(proxyService.ObjectMeta)),
		},
		&matchers.HttpResponse{
			StatusCode: code,
		},
		20*time.Second,
		2*time.Second,
	)
}

func (s *testingSuite) assertRouteHasNoStatus() {
	s.TestInstallation.Assertions.Gomega.Eventually(func(g gomega.Gomega) {
		route := &gwv1.HTTPRoute{}
		err := s.TestInstallation.ClusterContext.Client.Get(s.Ctx, types.NamespacedName{Name: "httpbin", Namespace: "httpbin"}, route)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get HTTPRoute")
		g.Expect(route.Status.Parents).To(gomega.BeEmpty())
	}, "120s", "1s").Should(gomega.Succeed())
}
