package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"gotest.tools/v3/assert"
)

func TestStoreIconTakesTheTokenAsAQueryParameter(t *testing.T) {
	r := newLogsRig(t)
	for _, c := range []struct {
		path   string
		status int
	}{
		{"/appstore/0/apps/immich/icon", http.StatusUnauthorized},
		{"/appstore/0/apps/immich/icon?token=bad", http.StatusUnauthorized},
		// a browser <img> cannot send Authorization: the JWT is in the query
		{"/appstore/0/apps/immich/icon?token=" + r.token, http.StatusNotFound},
		{"/appstore/0/apps/immich/screenshot/screenshot-1.png?token=" + r.token, http.StatusNotFound},
		// other appstore GETs still take the token only from the header
		{"/appstore?token=" + r.token, http.StatusUnauthorized},
		{"/apps?token=" + r.token, http.StatusUnauthorized},
	} {
		request := httptest.NewRequest(http.MethodGet, "/v2/app_management"+c.path, nil)
		request.RemoteAddr = "203.0.113.5:40000"
		recorder := httptest.NewRecorder()
		r.router.ServeHTTP(recorder, request)
		assert.Equal(t, recorder.Code, c.status, "%s: %s", c.path, recorder.Body.String())
	}
}

func TestStoreAssetGETsAcceptAQueryToken(t *testing.T) {
	e := echo.New()
	for _, path := range []string{
		V2APIPath + "/appstore/:id/apps/:app/icon",
		V2APIPath + "/appstore/:id/apps/:app/screenshot/:file",
	} {
		req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore/0/apps/immich/icon", nil)
		ctx := e.NewContext(req, httptest.NewRecorder())
		ctx.SetPath(path)
		assert.Assert(t, isStoreAsset(ctx), path)
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/app_management/appstore", nil)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.SetPath(V2APIPath + "/appstore")
	assert.Assert(t, !isStoreAsset(ctx))

	req = httptest.NewRequest(http.MethodPost, "/v2/app_management/appstore/0/apps/immich/icon", nil)
	ctx = e.NewContext(req, httptest.NewRecorder())
	ctx.SetPath(V2APIPath + "/appstore/:id/apps/:app/icon")
	assert.Assert(t, !isStoreAsset(ctx), "only GET")
}
