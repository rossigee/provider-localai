package apis

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	agentv1beta1 "github.com/rossigee/provider-localai/apis/agent/v1beta1"
	agenttaskv1beta1 "github.com/rossigee/provider-localai/apis/agenttask/v1beta1"
	collectionv1beta1 "github.com/rossigee/provider-localai/apis/collection/v1beta1"
	modelv1beta1 "github.com/rossigee/provider-localai/apis/model/v1beta1"
	settingsv1beta1 "github.com/rossigee/provider-localai/apis/settings/v1beta1"
	skillv1beta1 "github.com/rossigee/provider-localai/apis/skill/v1beta1"
	v1beta1 "github.com/rossigee/provider-localai/apis/v1beta1"
)

// TestAddToSchemeRegistersEveryAPIType guards the provider against shipping a
// type whose package is never registered: the provider then panics at startup
// with "no kind ... is registered for version ... in scheme" (this is what
// happened with Collection in v0.6.0).
func TestAddToSchemeRegistersEveryAPIType(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	objects := []runtime.Object{
		&v1beta1.ProviderConfig{},
		&v1beta1.ProviderConfigList{},
		&agentv1beta1.Agent{},
		&agentv1beta1.AgentList{},
		&skillv1beta1.Skill{},
		&skillv1beta1.SkillList{},
		&modelv1beta1.Model{},
		&modelv1beta1.ModelList{},
		&settingsv1beta1.Settings{},
		&settingsv1beta1.SettingsList{},
		&collectionv1beta1.Collection{},
		&collectionv1beta1.CollectionList{},
		&agenttaskv1beta1.AgentTask{},
		&agenttaskv1beta1.AgentTaskList{},
	}
	for _, obj := range objects {
		gvks, _, err := s.ObjectKinds(obj)
		if err != nil {
			t.Errorf("%T is not registered in the scheme: %v", obj, err)
			continue
		}
		if len(gvks) == 0 {
			t.Errorf("%T maps to no GroupVersionKinds", obj)
		}
	}
}
