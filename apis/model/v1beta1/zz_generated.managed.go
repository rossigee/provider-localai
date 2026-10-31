package v1beta1

import xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"

func (mg *Model) GetCondition(ct xpv2.ConditionType) xpv2.Condition {
	return mg.Status.GetCondition(ct)
}
func (mg *Model) GetManagementPolicies() xpv2.ManagementPolicies { return mg.Spec.ManagementPolicies }
func (mg *Model) GetProviderConfigReference() *xpv2.ProviderConfigReference {
	return mg.Spec.ProviderConfigReference
}
func (mg *Model) GetWriteConnectionSecretToReference() *xpv2.LocalSecretReference {
	return mg.Spec.WriteConnectionSecretToReference
}
func (mg *Model) SetConditions(c ...xpv2.Condition)               { mg.Status.SetConditions(c...) }
func (mg *Model) SetManagementPolicies(r xpv2.ManagementPolicies) { mg.Spec.ManagementPolicies = r }
func (mg *Model) SetProviderConfigReference(r *xpv2.ProviderConfigReference) {
	mg.Spec.ProviderConfigReference = r
}
func (mg *Model) SetWriteConnectionSecretToReference(r *xpv2.LocalSecretReference) {
	mg.Spec.WriteConnectionSecretToReference = r
}
