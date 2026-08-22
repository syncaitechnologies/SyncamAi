package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/privacymasks"
)

func privacyMaskHandler(principal identity.Principal, repository privacymasks.Repository) http.Handler {
	return NewWithPrivacyMaskApprovals(fakeVerifier{principal: principal}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, repository)
}

func TestPrivacyMaskRequestsRequireMFAAuthenticatedSuperAdmin(t *testing.T) {
	repository := privacymasks.NewMemoryRepository(nil)
	body := `{"site_id":"` + cameraSite + `","camera_id":"` + cameraID + `","name":"Entry mask","geometry":` + polygon() + `}`
	admin := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	admin.MFALevel = "t2"
	created := cameraRequest(privacyMaskHandler(admin, repository), http.MethodPost, "/v1/privacy-mask-requests", body, map[string]string{correlationHeader: "11111111-1111-4111-8111-111111111111"})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"status":"pending"`) {
		t.Fatalf("create privacy mask request: %d %s", created.Code, created.Body.String())
	}

	siteAdmin := cameraPrincipal(identity.RoleSiteAdmin, "privacy_masks:approve")
	siteAdmin.MFALevel = "t2"
	if denied := cameraRequest(privacyMaskHandler(siteAdmin, repository), http.MethodPost, "/v1/privacy-mask-requests", body, map[string]string{correlationHeader: "11111111-1111-4111-8111-111111111111"}); denied.Code != http.StatusForbidden {
		t.Fatalf("site admin must be denied: %d %s", denied.Code, denied.Body.String())
	}

	noMFA := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	if denied := cameraRequest(privacyMaskHandler(noMFA, repository), http.MethodPost, "/v1/privacy-mask-requests", body, map[string]string{correlationHeader: "11111111-1111-4111-8111-111111111111"}); denied.Code != http.StatusForbidden {
		t.Fatalf("non-MFA super admin must be denied: %d %s", denied.Code, denied.Body.String())
	}
}

func TestPrivacyMaskApprovalAPIEnforcesDistinctApprovers(t *testing.T) {
	repository := privacymasks.NewMemoryRepository(nil)
	body := `{"site_id":"` + cameraSite + `","camera_id":"` + cameraID + `","name":"Entry mask","geometry":` + polygon() + `}`
	headers := map[string]string{correlationHeader: "11111111-1111-4111-8111-111111111111"}
	requester := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	requester.UserID, requester.MFALevel = "requester", "t2"
	created := cameraRequest(privacyMaskHandler(requester, repository), http.MethodPost, "/v1/privacy-mask-requests", body, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create request: %d %s", created.Code, created.Body.String())
	}
	var envelope struct{ Data struct{ ID, Status string } }
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil || envelope.Data.ID == "" {
		t.Fatalf("decode created request: %v %s", err, created.Body.String())
	}
	path := "/v1/privacy-mask-requests/" + envelope.Data.ID
	if got := cameraRequest(privacyMaskHandler(requester, repository), http.MethodGet, path, "", nil); got.Code != http.StatusOK {
		t.Fatalf("get request: %d %s", got.Code, got.Body.String())
	}
	if denied := cameraRequest(privacyMaskHandler(requester, repository), http.MethodPost, path+"/approvals", "", headers); denied.Code != http.StatusConflict {
		t.Fatalf("requester approval must conflict: %d %s", denied.Code, denied.Body.String())
	}
	first := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	first.UserID, first.MFALevel = "approver-a", "t2"
	if approved := cameraRequest(privacyMaskHandler(first, repository), http.MethodPost, path+"/approvals", "", headers); approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"status":"pending"`) {
		t.Fatalf("first approval: %d %s", approved.Code, approved.Body.String())
	}
	second := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	second.UserID, second.MFALevel = "approver-b", "t2"
	if approved := cameraRequest(privacyMaskHandler(second, repository), http.MethodPost, path+"/approvals", "", headers); approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"status":"approved"`) {
		t.Fatalf("second approval: %d %s", approved.Code, approved.Body.String())
	}
	third := cameraPrincipal(identity.RoleSuperAdmin, "privacy_masks:approve")
	third.UserID, third.MFALevel = "approver-c", "t2"
	if denied := cameraRequest(privacyMaskHandler(third, repository), http.MethodPost, path+"/approvals", "", headers); denied.Code != http.StatusConflict {
		t.Fatalf("surplus approval must conflict: %d %s", denied.Code, denied.Body.String())
	}
}
