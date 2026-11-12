package skill

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laiclient "github.com/rossigee/localai-client/client"
	skillv1beta1 "github.com/rossigee/provider-localai/apis/skill/v1beta1"
	"github.com/rossigee/provider-localai/internal/clients"
)

func skillCR(name string) *skillv1beta1.Skill {
	return &skillv1beta1.Skill{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: name},
		Spec:       skillv1beta1.SkillSpec{ForProvider: skillv1beta1.SkillParameters{Name: name, Content: "c"}},
	}
}

func skillExternal(t *testing.T, h http.HandlerFunc) *external {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := laiclient.New(context.Background(), laiclient.Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &external{client: c}
}

func TestDeleteKeepsAdoptedSkill(t *testing.T) {
	var writes int
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		_, _ = io.WriteString(w, `{}`)
	})
	cr := skillCR("research")
	cr.Status.AtProvider.Ownership = clients.OwnershipAdopted
	if _, err := e.Delete(context.Background(), cr); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if writes != 0 {
		t.Errorf("Delete sent %d write request(s) for an adopted skill, want 0", writes)
	}
}

func TestObserveMarksPreExistingSkillAdopted(t *testing.T) {
	e := skillExternal(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"name":"research","content":"c"}`)
	})
	cr := skillCR("research")
	if _, err := e.Observe(context.Background(), cr); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if cr.Status.AtProvider.Ownership != clients.OwnershipAdopted {
		t.Errorf("ownership = %q, want adopted", cr.Status.AtProvider.Ownership)
	}
}
