# EP-10651: JWT-based Authentication for Envoy to kgateway Communication

* Issue: [#10651](URL to GitHub issue)

## Background

Currently, there is no authentication mechanism between Envoy and kgateway. This EP proposes implementing JWT-based authentication where Envoy presents a Kubernetes-issued JWT to kgateway, which then validates the token and uses it to determine the appropriate configuration to serve.

## Motivation

Implementing authentication between Envoy and kgateway is crucial for:
- Ensuring only authorized Envoy instances can connect to kgateway
- Enabling configuration isolation based on the Envoy instance's identity
- Leveraging Kubernetes' built-in service account token issuance
- Providing a secure foundation for future authorization enhancements

### Goals


1. Implement JWT-based authentication where Envoy presents a Kubernetes-issued token
2. Configure Envoy to include the JWT in gRPC requests to kgateway
3. Implement JWT validation in kgateway using OpenID Connect discovery
4. Map validated JWT claims to Kubernetes resources (Pod → Deployment → Gateway)
5. Restrict configuration access based on the Envoy's identity

### Non-Goals 

- Implementing custom token issuance
- Supporting multiple authentication methods


## Implementation Details

### Configuration

1. New Helm values:
```yaml
auth:
  enabled: true
  audience: kgateway  # JWT audience claim
```

### Control plane

The authentication will be handled by a new kgateway auth component for the control plane:
- Extract JWT from gRPC metadata
- Validate JWT using OpenID Connect discovery
- Map JWT claims to Kubernetes resources
- Implement gRPC interceptor for authentication

### Controllers

No new controllers required. Existing controllers will:
- Watch for changes to Gateway resources
- Update configuration based on authenticated identity

### Deployer

The deployer will be updated to:
- Configure Envoy with the service account token path
- Set up JWT authentication in Envoy's gRPC client
- Include audience claim in JWT token service account projection

Example pod jwt volume:

```yaml
volumes:
- name: token-vol
  projected:
    sources:
    - serviceAccountToken:
        audience: kgateway
        expirationSeconds: 3600
        path: token
```

### Envoy

Envoy's bootstrap config will be updated to:
- Include JWT in gRPC requests to kgateway
- When kubelet rotates the service account token, envoy will pick up the new token automatically and
  use it for the next request.

### Authentication Flow

1. Envoy starts with a mounted service account token
2. Envoy includes the JWT in gRPC metadata when connecting to kgateway
3. kgateway extracts the JWT and validates it:
   - Verifies signature using OpenID Connect discovery
     - issuer url can be configured, defaults to the issuer from our own JWT (similar to how istiod does it)
   - Checks audience claim matches "kgateway"
   - Validates token expiration (allowing for clock skew)
4. kgateway extracts pod name/namespace from the JWT's claims
5. kgateway follows the ownerReferences from the pod->RS->Deployment->Gateway. This will tell kgateway which Gateway to serve.
6. kgateway serves only the relevant Gateway configuration
7. We can ignore current role metadata present in the envoy's node metadata.

### Test Plan 

1. Unit Tests:
   - JWT validation logic
   - OpenID Connect discovery handling
   - gRPC interceptor functionality

2. Integration Tests:
   - We may need to adjust setup tests to use JWTs auth (or leave the metadata role available as a fallback)

3. E2E Tests:
   - Complete authentication flow
   - Token rotation scenarios

## Alternatives

1. Using mTLS for authentication:
   - We will need to distribute the client certs, a problem that k8s solves for us. so we 
     get exrta complexity with no additional value.

2. Custom token issuance:
   - Pros: Could simplify auth flow by creating a JWT with the gateway name as a claim.
   - Cons: Complex issuance and token distribution needed. reinventing the wheel

## Open Questions

1. How should we handle token expiration and rotation?
2. How/if should handle VMs? perhaps leave existing method of roles as a fallback?
3. Should jwt validation be a grpc interceptor or an envoy xDS callbacks?