package apis

import (
	agentv1beta1 "github.com/rossigee/provider-localai/apis/agent/v1beta1"
	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
	settingsv1beta1 "github.com/rossigee/provider-localai/apis/settings/v1beta1"
	skillv1beta1 "github.com/rossigee/provider-localai/apis/skill/v1beta1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	AddToSchemes = append(AddToSchemes,
		agentv1beta1.AddToScheme,
		skillv1beta1.AddToScheme,
		modelv1beta1.AddToScheme,
		settingsv1beta1.AddToScheme,
		v1beta1.AddToScheme,
	)
}

var AddToSchemes runtime.SchemeBuilder

func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
