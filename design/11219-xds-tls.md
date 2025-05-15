# EP-11219: TLS Support for kgateway-Envoy Communication

* Issue: [#11219](URL to GitHub issue)


## Background


Currently, the communication between kgateway and Envoy is unencrypted. This EP proposes adding TLS support to secure this communication channel. The implementation will focus on one-way TLS where kgateway presents a certificate that Envoy verifies using a provided CA certificate.

## Motivation

Securing the communication between kgateway and Envoy is required for:
- Protecting sensitive configuration data in transit
- Meeting security compliance requirements

### Goals


1. Implement one-way TLS where kgateway presents a certificate to Envoy
2. Allow configuration of TLS certificates through Kubernetes secrets
3. Automatically propagate CA certificate to Envoy's bootstrap configuration
4. Support TLS configuration through Helm values

### Non-Goals

- Mutual TLS authentication (mTLS)
- Dynamic certificate rotation
- Certificate management automation
- Support anything other than k8s secrets for storing the certificates

## Implementation Details

### Configuration

1. New Helm values:
```yaml
tls:
  enabled: true
  secretName: kgateway-tls-secret  # Name of the secret containing certs
```

2. Required secret format:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kgateway-tls-secret
type: Opaque
data:
  tls.crt: <base64 encoded server certificate>
  tls.key: <base64 encoded private key>
  ca.crt: <base64 encoded CA certificate>
```

### Server

The cert secret will be mounted into the kgateway xDS server pod.

The TLS configuration will be handled by the kgateway xDS server:
- Load TLS certificates from the specified secret. 
  - if the secret is not found, the server will not start.
  - if the secret changes, the server will use the updated certificates (with no restart).
- Configure the gRPC server with TLS settings

### Controllers

No new controllers required, as the secret is mounted into the kgateway xDS server pod.

### Deployer

The deployer will be updated to:
- Read the CA certificate from the secret
- Inject the CA certificate into Envoy's bootstrap configuration
- Update the bootstrap configuration to enable TLS verification

### Test Plan

1. Unit Tests:
   - As needed

2. E2E Tests:
   - Complete deployment with TLS enabled
   - Communication verification
   - Certificate rotation works, and the new certificates are used after rotation.

## Alternatives


1. Using ConfigMaps for the CA instead of placing it in Secret:
   - Pros: CA is technically not a secret, and can be placed in a ConfigMap.
   - Cons: One more resource to manage.

2. Mutual TLS:
   - Pros: Unclear if mTLS is more secure in this use case, using the k8s jwt should suffice for auth (see ep-10651).
   - Cons: More complex to manage.

## Open Questions

1. Any reason to use TLS version lower than TLS1.3?
2. Should we limit the default TLS cipher suites?
