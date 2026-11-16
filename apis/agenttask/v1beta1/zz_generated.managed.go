package v1beta1

import xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"

func (mg *AgentTask) GetCondition(ct xpv2.ConditionType) xpv2.Condition {
	return mg.Status.GetCondition(ct)
}
func (mg *AgentTask) GetManagementPolicies() xpv2.ManagementPolicies {
	return mg.Spec.ManagementPolicies
}
func (mg *AgentTask) GetProviderConfigReference() *xpv2.ProviderConfigReference {
	return mg.Spec.ProviderConfigReference
}
func (mg *AgentTask) GetWriteConnectionSecretToReference() *xpv2.LocalSecretReference {
	return mg.Spec.WriteConnectionSecretToReference
}
func (mg *AgentTask) SetConditions(c ...xpv2.Condition) { mg.Status.SetConditions(c...) }
func (mg *AgentTask) SetManagementPolicies(r xpv2.ManagementPolicies) {
	mg.Spec.ManagementPolicies = r
}
func (mg *AgentTask) SetProviderConfigReference(r *xpv2.ProviderConfigReference) {
	mg.Spec.ProviderConfigReference = r
}
func (mg *AgentTask) SetWriteConnectionSecretToReference(r *xpv2.LocalSecretReference) {
	mg.Spec.WriteConnectionSecretToReference = r
}
