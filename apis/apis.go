package apis

import (
	agentv1alpha1 "github.com/rossigee/provider-localai/apis/agent/v1alpha1"
	modelv1alpha1 "github.com/rossigee/provider-localai/apis/model/v1alpha1"
	settingsv1alpha1 "github.com/rossigee/provider-localai/apis/settings/v1alpha1"
	skillv1alpha1 "github.com/rossigee/provider-localai/apis/skill/v1alpha1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	AddToSchemes = append(AddToSchemes,
		agentv1alpha1.AddToScheme,
		skillv1alpha1.AddToScheme,
		modelv1alpha1.AddToScheme,
		settingsv1alpha1.AddToScheme,
		v1beta1.AddToScheme,
	)
}

var AddToSchemes runtime.SchemeBuilder

func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
