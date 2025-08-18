package v1alpha1

// Rbac defines the configuration for role-based access control.
type Rbac struct {
	// Policies defines a list of roles and the principals that are assigned/denied the role.
	// A policy matches if and only if at least one of its permissions match the action taking place
	// AND at least one of its principals match the downstream
	// AND the condition is true if specified.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Policies []RbacPolicy `json:"policies"`

	// Action defines whether the rule allows or denies the request if matched.
	// If unspecified, the default is "Allow".
	// +kubebuilder:validation:Enum=Allow;Deny
	// +kubebuilder:default=Allow
	Action AuthorizationPolicyAction `json:"action,omitempty"`
}

// RbacPolicy defines a single RBAC rule.
type RbacPolicy struct {
	// CelMatchExpression defines a set of conditions that must be satisfied for the rule to match.
	// +kubebuilder:validation:MinItems=1
	CelMatchExpression []string `json:"matchExpressions,omitempty"`
}

type AuthorizationPolicyAction string

const (
	AuthorizationPolicyActionAllow AuthorizationPolicyAction = "Allow"
	AuthorizationPolicyActionDeny  AuthorizationPolicyAction = "Deny"
)
