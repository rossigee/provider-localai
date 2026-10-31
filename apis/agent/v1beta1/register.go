package v1beta1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	Group   = "agent.localai.m.crossplane.io"
	Version = "v1alpha1"
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}
	SchemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme        = SchemeBuilder.AddToScheme
)

var (
	AgentKind             = reflect.TypeOf(Agent{}).Name()
	AgentGroupKind        = schema.GroupKind{Group: Group, Kind: AgentKind}.String()
	AgentKindAPIVersion   = AgentKind + "." + SchemeGroupVersion.String()
	AgentGroupVersionKind = SchemeGroupVersion.WithKind(AgentKind)
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(SchemeGroupVersion,
		&Agent{},
		&AgentList{},
	)
	metav1.AddToGroupVersion(s, SchemeGroupVersion)
	return nil
}
