package rate_limit

import (
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	v1alpha1 "github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
)

const (
	// test namespace for proxy resources
	namespace = "kgateway-system"
	// test service name
	serviceName = "backend-0"
)

var (
	// paths to test manifests
	simpleServiceManifest     = testdata("service.yaml")
	commonManifest            = testdata("common.yaml")
	httpRoutesManifest        = testdata("routes.yaml")
	ipRateLimitManifest       = testdata("ip-rate-limit.yaml")
	pathRateLimitManifest     = testdata("path-rate-limit.yaml")
	userRateLimitManifest     = testdata("user-rate-limit.yaml")
	combinedRateLimitManifest = testdata("combined-rate-limit.yaml")
	failOpenRateLimitManifest = testdata("fail-open-rate-limit.yaml")
	rateLimitServiceManifest  = testdata("rate-limit-service.yaml")

	// metadata for gateway
	gatewayObjectMeta = metav1.ObjectMeta{Name: "kgateway", Namespace: namespace}
	gateway           = &gwv1.Gateway{
		ObjectMeta: gatewayObjectMeta,
	}

	// metadata for proxy resources
	proxyObjectMeta = metav1.ObjectMeta{Name: "kgateway", Namespace: namespace}

	proxyDeployment = &appsv1.Deployment{
		ObjectMeta: proxyObjectMeta,
	}
	proxyService = &corev1.Service{
		ObjectMeta: proxyObjectMeta,
	}
	proxyServiceAccount = &corev1.ServiceAccount{
		ObjectMeta: proxyObjectMeta,
	}

	// metadata for backend service
	serviceMeta = metav1.ObjectMeta{
		Namespace: namespace,
		Name:      serviceName,
	}

	simpleSvc = &corev1.Service{
		ObjectMeta: serviceMeta,
	}

	simpleDeployment = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      serviceName,
		},
	}

	// metadata for rate limit service
	rateLimitObjectMeta = metav1.ObjectMeta{Name: "ratelimit", Namespace: namespace}

	rateLimitDeployment = &appsv1.Deployment{
		ObjectMeta: rateLimitObjectMeta,
	}
	rateLimitService = &corev1.Service{
		ObjectMeta: rateLimitObjectMeta,
	}
	rateLimitConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ratelimit-config", Namespace: namespace},
	}

	// metadata for httproutes
	route = &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-route-1",
		},
	}

	route2 = &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-route-2",
		},
	}

	// Gateway Extension for rate limit service
	gatewayExtension = &v1alpha1.GatewayExtension{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "global-ratelimit",
		},
	}

	// Traffic Policies for different rate limit scenarios
	ipRateLimitTrafficPolicy = &v1alpha1.TrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "ip-rate-limit",
		},
	}

	pathRateLimitTrafficPolicy = &v1alpha1.TrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "path-rate-limit",
		},
	}

	userRateLimitTrafficPolicy = &v1alpha1.TrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "user-rate-limit",
		},
	}

	combinedRateLimitTrafficPolicy = &v1alpha1.TrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "combined-rate-limit",
		},
	}

	failOpenRateLimitTrafficPolicy = &v1alpha1.TrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "fail-open-rate-limit",
		},
	}
)

func testdata(filename string) string {
	return filepath.Join(".", "testdata", filename)
}
