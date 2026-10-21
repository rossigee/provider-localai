package controller

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/rossigee/provider-localai/internal/controller/agent"
	"github.com/rossigee/provider-localai/internal/controller/model"
	"github.com/rossigee/provider-localai/internal/controller/providerconfig"
	"github.com/rossigee/provider-localai/internal/controller/settings"
	"github.com/rossigee/provider-localai/internal/controller/skill"
)

// Setup sets up LocalAI provider controllers.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	if err := providerconfig.Setup(mgr); err != nil {
		return err
	}
	if err := agent.Setup(mgr, o); err != nil {
		return err
	}
	if err := skill.Setup(mgr, o); err != nil {
		return err
	}
	if err := model.Setup(mgr, o); err != nil {
		return err
	}
	if err := settings.Setup(mgr, o); err != nil {
		return err
	}
	return nil
}
