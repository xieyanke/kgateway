package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// +kubebuilder:rbac:groups=gateway.kgateway.dev,resources=gatewayextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.kgateway.dev,resources=gatewayextensions/status,verbs=get;update;patch

// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=".spec.type",description="Which extension type?"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp",description="The age of the gatewayextension."

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels={app=kgateway,app.kubernetes.io/name=kgateway}
// +kubebuilder:resource:categories=kgateway
// +kubebuilder:subresource:status
type GatewayExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayExtensionSpec   `json:"spec,omitempty"`
	Status GatewayExtensionStatus `json:"status,omitempty"`
}

// GatewayExtensionType indicates the type of the GatewayExtension.
type GatewayExtensionType string

const (
	// GatewayExtensionTypeExtAuth is the type for Extauth extensions.
	GatewayExtensionTypeExtAuth GatewayExtensionType = "ExtAuth"
	// GatewayExtensionTypeExtProc is the type for ExtProc extensions.
	GatewayExtensionTypeExtProc GatewayExtensionType = "ExtProc"
	// GatewayExtensionTypeRateLimit is the type for RateLimit extensions.
	GatewayExtensionTypeRateLimit GatewayExtensionType = "RateLimit"
)

// ExtAuthProvider defines the configuration for an ExtAuth provider.
type ExtAuthProvider struct {
	// GrpcService is the GRPC service that will handle the authentication.
	// +required
	GrpcService *ExtGrpcService `json:"grpcService"`
}

// ExtProcProvider defines the configuration for an ExtProc provider.
type ExtProcProvider struct {
	// GrpcService is the GRPC service that will handle the processing.
	// +required
	GrpcService *ExtGrpcService `json:"grpcService"`
}

// ExtGrpcService defines the GRPC service that will handle the processing.
type ExtGrpcService struct {
	// BackendRef references the backend GRPC service.
	// +required
	BackendRef *gwv1.BackendRef `json:"backendRef"`

	// Authority is the authority header to use for the GRPC service.
	// +optional
	Authority *string `json:"authority,omitempty"`
}

// RateLimitProvider defines the configuration for a RateLimit service provider.
type RateLimitProvider struct {
	// GrpcService is the GRPC service that will handle the rate limiting.
	// +required
	GrpcService *ExtGrpcService `json:"grpcService"`

	// Domain identifies a rate limiting configuration for the rate limit service.
	// All rate limit requests must specify a domain, which enables the configuration
	// to be per application without fear of overlap (e.g., "api", "web", "admin").
	// +required
	Domain string `json:"domain"`

	// FailOpen determines if requests are limited when the rate limit service is unavailable.
	// When true, requests are not limited if the rate limit service is unavailable.
	// +optional
	// +kubebuilder:default=false
	FailOpen bool `json:"failOpen,omitempty"`

	// Timeout for requests to the rate limit service.
	// +optional
	// +kubebuilder:validation:XValidation:rule="matches(self, '^([0-9]{1,5}(h|m|s|ms)){1,4}$')",message="invalid duration value"
	// +kubebuilder:default="20ms"
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// GatewayExtensionSpec defines the desired state of GatewayExtension.
// +kubebuilder:validation:XValidation:message="ExtAuth must be set when type is ExtAuth",rule="self.type != 'ExtAuth' || has(self.extAuth)"
// +kubebuilder:validation:XValidation:message="ExtProc must be set when type is ExtProc",rule="self.type != 'ExtProc' || has(self.extProc)"
// +kubebuilder:validation:XValidation:message="RateLimit must be set when type is RateLimit",rule="self.type != 'RateLimit' || has(self.rateLimit)"
// +kubebuilder:validation:XValidation:message="ExtAuth must not be set when type is not ExtAuth",rule="self.type == 'ExtAuth' || !has(self.extAuth)"
// +kubebuilder:validation:XValidation:message="ExtProc must not be set when type is not ExtProc",rule="self.type == 'ExtProc' || !has(self.extProc)"
// +kubebuilder:validation:XValidation:message="RateLimit must not be set when type is not RateLimit",rule="self.type == 'RateLimit' || !has(self.rateLimit)"
type GatewayExtensionSpec struct {
	// Type indicates the type of the GatewayExtension to be used.
	// +unionDiscriminator
	// +kubebuilder:validation:Enum=ExtAuth;ExtProc;RateLimit
	// +required
	Type GatewayExtensionType `json:"type"`

	// ExtAuth configuration for ExtAuth extension type.
	// +optional
	// +unionMember:type=ExtAuth
	ExtAuth *ExtAuthProvider `json:"extAuth,omitempty"`

	// ExtProc configuration for ExtProc extension type.
	// +optional
	// +unionMember:type=ExtProc
	ExtProc *ExtProcProvider `json:"extProc,omitempty"`

	// RateLimit configuration for RateLimit extension type.
	// +optional
	// +unionMember:type=RateLimit
	RateLimit *RateLimitProvider `json:"rateLimit,omitempty"`
}

// GatewayExtensionStatus defines the observed state of GatewayExtension.
type GatewayExtensionStatus struct {
	// Conditions is the list of conditions for the GatewayExtension.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type GatewayExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayExtension `json:"items"`
}
