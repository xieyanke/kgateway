# Global Rate Limiting in kgateway

Global rate limiting allows you to apply distributed, consistent rate limits across multiple instances of your gateway. Unlike local rate limiting, which operates independently on each gateway instance, global rate limiting uses a central service to coordinate rate limits.

## Overview

Global rate limiting in kgateway is powered by [Envoy's rate limiting service protocol](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) and delegates rate limit decisions to an external service that implements this protocol. This approach provides several benefits:

- **Coordinated rate limiting** across multiple gateway instances
- **Centralized rate limit management** with shared counters
- **Dynamic descriptor-based rate limits** that can consider multiple request attributes
- **Consistent user experience** regardless of which gateway instance receives the request

## How It Works

1. When a request arrives at kgateway, the gateway extracts descriptor information based on your policy configuration
2. kgateway sends these descriptors to your rate limit service
3. The rate limit service applies configured limits for those descriptors and returns a decision
4. kgateway either allows or denies the request based on the service's decision

## Architecture

The global rate limiting feature consists of three components:

1. **TrafficPolicy with rateLimit.global** - Configures which descriptors to extract and send to the rate limit service
2. **GatewayExtension** - References the rate limit service implementation
3. **Rate Limit Service** - An external service that implements the Envoy Rate Limit protocol and contains the actual rate limit values

> **Important**: A TrafficPolicy must be in the same namespace as any GatewayExtension it references. When a TrafficPolicy references a GatewayExtension via the `extensionRef` field, kgateway will look for that extension in the same namespace as the TrafficPolicy.

## Deployment

### 1. Deploy the Rate Limit Service

kgateway integrates with any service that implements the Envoy Rate Limit gRPC protocol. For your convenience, we provide an example deployment using the official Envoy rate limit service in the [test/kubernetes/e2e/features/rate_limit/testdata](../test/kubernetes/e2e/features/rate_limit/testdata) directory.

```bash
kubectl apply -f test/kubernetes/e2e/features/rate_limit/testdata/rate-limit-service.yaml
```

### 2. Configure the Rate Limit Service

The actual rate limit values (requests per unit time) must be configured in the rate limit service, not in kgateway's policies. For example, using the [Envoy Rate Limit](https://github.com/envoyproxy/ratelimit) service, you would configure limits in its configuration file:

```yaml
# ratelimit-config.yaml
domain: api-gateway
descriptors:
  - key: remote_address
    rate_limit:
      unit: minute
      requests_per_unit: 1
  - key: path
    value: /path1
    rate_limit:
      unit: minute
      requests_per_unit: 1
  - key: user_id
    rate_limit:
      unit: minute
      requests_per_unit: 1
```

### 3. Create a GatewayExtension

The GatewayExtension resource connects your kgateway installation with the rate limit service:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: GatewayExtension
metadata:
  name: global-ratelimit
  namespace: kgateway-system
spec:
  type: RateLimit
  rateLimit:
    grpcService:
      backendRef:
        name: ratelimit
        namespace: kgateway-system
        port: 8081
    domain: "api-gateway"
    timeout: "100ms"  # Optional timeout for rate limit service calls
```

### 4. Create TrafficPolicies with Global Rate Limiting

Apply rate limits to your routes using the TrafficPolicy resource:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: ip-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "remote_address"
        valueFrom:
          remoteAddress: true
      extensionRef:
        name: global-ratelimit
      failOpen: false
```

## Configuration Options

### TrafficPolicy.spec.rateLimit.global

| Field | Description | Required |
|-------|-------------|----------|
| domain | Identifies a rate limiting configuration domain | Yes |
| descriptors | Define the dimensions for rate limiting | Yes |
| extensionRef | Reference to a GatewayExtension for the rate limit service | Yes |
| failOpen | When true, requests are not limited if the rate limit service is unavailable | No |

### Rate Limit Descriptors

Descriptors define the dimensions for rate limiting. Each descriptor represents a key-value pair that helps categorize and count requests:

```yaml
descriptors:
- key: "remote_address"
  valueFrom:
    remoteAddress: true
- key: "path"
  valueFrom:
    path: true
- key: "user_id"
  valueFrom:
    header: "X-User-ID"
- key: "service"
  value: "premium-api"
```

## Examples

The [test/kubernetes/e2e/features/rate_limit/testdata](../test/kubernetes/e2e/features/rate_limit/testdata) directory contains several examples:

1. **IP-based rate limiting** ([ip-rate-limit.yaml](../test/kubernetes/e2e/features/rate_limit/testdata/ip-rate-limit.yaml)): Limit requests based on client IP address
2. **Path-based rate limiting** ([path-rate-limit.yaml](../test/kubernetes/e2e/features/rate_limit/testdata/path-rate-limit.yaml)): Apply different limits to specific paths
3. **User-based rate limiting** ([user-rate-limit.yaml](../test/kubernetes/e2e/features/rate_limit/testdata/user-rate-limit.yaml)): Rate limit based on a user identifier header
4. **Combined rate limiting** ([combined-rate-limit.yaml](../test/kubernetes/e2e/features/rate_limit/testdata/combined-rate-limit.yaml)): Use both global and local rate limiting together
5. **Fail open rate limiting** ([fail-open-rate-limit.yaml](../test/kubernetes/e2e/features/rate_limit/testdata/fail-open-rate-limit.yaml)): Configure rate limiter to allow requests when service is unavailable

## Example Configurations

### Example 1: IP-Based Rate Limiting

**Rate Limit Service Configuration:**
```yaml
domain: api-gateway
descriptors:
  - key: remote_address
    rate_limit:
      unit: minute
      requests_per_unit: 1
```

**TrafficPolicy:**
```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: ip-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "remote_address"
        valueFrom:
          remoteAddress: true
      extensionRef:
        name: global-ratelimit
```

### Example 2: Path-Based Rate Limiting

**Rate Limit Service Configuration:**
```yaml
domain: api-gateway
descriptors:
  - key: path
    value: /path1
    rate_limit:
      unit: minute
      requests_per_unit: 1
  - key: path
    value: /path2
    rate_limit:
      unit: minute
      requests_per_unit: 5
```

**TrafficPolicy:**
```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: path-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-2
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "path"
        valueFrom:
          path: true
      extensionRef:
        name: global-ratelimit
```

### Example 3: User-Based Rate Limiting

**Rate Limit Service Configuration:**
```yaml
domain: api-gateway
descriptors:
  - key: user_id
    rate_limit:
      unit: minute
      requests_per_unit: 1
```

**TrafficPolicy:**
```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: user-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "user_id"
        valueFrom:
          header: "X-User-ID"
      extensionRef:
        name: global-ratelimit
      failOpen: false
```

### Example 4: Combined Rate Limiting

This example shows how to use both global and local rate limiting together:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: combined-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "service"
        value: "premium-api"
      extensionRef:
        name: global-ratelimit
    local:
      tokenBucket:
        maxTokens: 5
        tokensPerFill: 1
        fillInterval: "1s"
```

### Example 5: Fail Open Rate Limiting

This example configures the rate limiter to allow requests when the rate limit service is unavailable:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: fail-open-rate-limit
  namespace: kgateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: test-route-1
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "remote_address"
        valueFrom:
          remoteAddress: true
      extensionRef:
        name: global-ratelimit
      failOpen: true
```
