package tracing

import (
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/fsutils"
	e2edefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests/base"
)

var (
	// manifests
	setupManifest         = filepath.Join(fsutils.MustGetThisDir(), "testdata", "setup.yaml")
	otelCollectorManifest = filepath.Join(fsutils.MustGetThisDir(), "testdata", "otlpeek.yaml")
	policyManifest        = filepath.Join(fsutils.MustGetThisDir(), "testdata", "tracing-policy.yaml")

	// setup objects
	proxyObjectMeta = metav1.ObjectMeta{
		Name:      "gw",
		Namespace: "default",
	}
	proxyDeployment = &appsv1.Deployment{ObjectMeta: proxyObjectMeta}
	proxyService    = &corev1.Service{ObjectMeta: proxyObjectMeta}

	httpbinSvc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "httpbin",
			Namespace: "httpbin",
		},
	}
	httpbinDeployment = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "httpbin",
			Namespace: "httpbin",
		},
	}

	// otelCollector objects
	otelCollectorDeployment = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "otlpeek",
			Namespace: "default",
		},
	}

	tracingPolicy = &v1alpha1.HTTPListenerPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tracing-policy",
			Namespace: "default",
		},
	}

	setup = base.SimpleTestCase{
		Manifests: []string{e2edefaults.CurlPodManifest, setupManifest},
		Resources: []client.Object{e2edefaults.CurlPod, proxyDeployment, proxyService, httpbinSvc, httpbinDeployment},
	}

	// test cases
	testCases = map[string]*base.TestCase{
		"TestOTelTracing": {
			SimpleTestCase: base.SimpleTestCase{
				Manifests: []string{otelCollectorManifest, policyManifest},
				Resources: []client.Object{otelCollectorDeployment, tracingPolicy},
			},
		},
	}
)
