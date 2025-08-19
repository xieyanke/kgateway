package v1alpha1

// RBAC defines the configuration for role-based access control.
type RBAC struct {
	// Policies defines a list of roles and the principals that are assigned/denied the role.
	// A policy matches if and only if at least one of its permissions match the action taking place
	// AND at least one of its principals match the downstream
	// AND the condition is true if specified.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Policies []RBACPolicy `json:"policies"`

	// Action defines whether the rule allows or denies the request if matched.
	// If unspecified, the default is "Allow".
	// +kubebuilder:validation:Enum=Allow;Deny
	// +kubebuilder:default=Allow
	Action AuthorizationPolicyAction `json:"action,omitempty"`

	// Disable the RBAC filter.
	// Can be used to disable RBAC policies applied at a higher level in the config hierarchy.
	// +optional
	Disable *PolicyDisable `json:"disable,omitempty"`
}

// RBACPolicy defines a single RBAC rule.
type RBACPolicy struct {
	// MatchExpressions defines a set of conditions that must be satisfied for the rule to match.
	// These expression should be in the form of a Common Expression Language (CEL) expression.
	// See: https://www.envoyproxy.io/docs/envoy/latest/xds/type/matcher/v3/cel.proto
	// +kubebuilder:validation:MinItems=1
	MatchExpressions []string `json:"matchExpressions,omitempty"`
}

// AuthorizationPolicyAction defines the action to take when the RBACPolicies matches.
type AuthorizationPolicyAction string

const (
	// AuthorizationPolicyActionAllow defines the action to take when the RBACPolicies matches.
	AuthorizationPolicyActionAllow AuthorizationPolicyAction = "Allow"
	// AuthorizationPolicyActionDeny denies the action to take when the RBACPolicies matches.
	AuthorizationPolicyActionDeny AuthorizationPolicyAction = "Deny"
)
