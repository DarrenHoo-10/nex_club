package admin

import (
	"net/http"

	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
)

func Mount(mux *http.ServeMux, d deps.Deps) {
	if d.Store != nil {
		Use(d)
		installAuth(d)
	}
	mux.HandleFunc("POST /api/admin/session", Login)
	mux.Handle("DELETE /api/admin/session", Protect(http.HandlerFunc(Logout)))
	mux.Handle("GET /api/admin/session", Protect(http.HandlerFunc(Current)))

	mux.Handle("GET /api/admin/resources", Protect(http.HandlerFunc(ListResources)))
	mux.Handle("POST /api/admin/resources", Protect(http.HandlerFunc(CreateResource)))
	mux.Handle("GET /api/admin/resources/{id}", Protect(http.HandlerFunc(GetResource)))
	mux.Handle("PUT /api/admin/resources/{id}", Protect(http.HandlerFunc(SaveResource)))
	mux.Handle("GET /api/admin/resources/{id}/revisions", Protect(http.HandlerFunc(ListRevisions)))
	mux.Handle("GET /api/admin/resources/{id}/preview", Protect(http.HandlerFunc(PreviewResource)))
	mux.Handle("POST /api/admin/resources/{id}/publish", Protect(http.HandlerFunc(PublishResource)))
	mux.Handle("POST /api/admin/resources/{id}/visibility", Protect(http.HandlerFunc(SetVisibility)))

	mux.Handle("GET /api/admin/tags", Protect(http.HandlerFunc(ListTags)))
	mux.Handle("POST /api/admin/tags", Protect(http.HandlerFunc(CreateTag)))
	mux.Handle("POST /api/admin/tags/{id}/merge", Protect(http.HandlerFunc(MergeTag)))

	mux.Handle("GET /api/admin/featured", Protect(http.HandlerFunc(ListFeatured)))
	mux.Handle("POST /api/admin/featured", Protect(http.HandlerFunc(CreateFeatured)))
	mux.Handle("DELETE /api/admin/featured/{id}", Protect(http.HandlerFunc(DisableFeatured)))

	mux.Handle("GET /api/admin/audit", Protect(http.HandlerFunc(ListAudit)))
	MountAutomation(mux)
	MountProposals(mux)
	MountProviderCalls(mux)
	mountMCP(mux)
}
