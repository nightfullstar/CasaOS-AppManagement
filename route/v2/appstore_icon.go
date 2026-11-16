package v2

import (
	"net/http"
	"os"

	"github.com/ReCasaOS/CasaOS-AppManagement/codegen"
	"github.com/ReCasaOS/CasaOS-AppManagement/service"
	"github.com/ReCasaOS/CasaOS-Common/utils"
	"github.com/labstack/echo/v4"
)

func (a *AppManagement) AppStoreAppIcon(ctx echo.Context, id codegen.AppStoreID, app codegen.AppStoreAppID) error {
	if service.MyService == nil {
		return iconNotFound(ctx)
	}
	path, contentType, err := service.MyService.AppStoreManagement().IconFile(id, app)
	if err != nil {
		return iconNotFound(ctx)
	}
	return sendAppStoreIcon(ctx, path, contentType)
}

func serveAppStoreIcon(ctx echo.Context, workdir, app string) error {
	path, contentType, err := service.FindStoreAppIcon(workdir, app)
	if err != nil {
		return iconNotFound(ctx)
	}
	return sendAppStoreIcon(ctx, path, contentType)
}

func sendAppStoreIcon(ctx echo.Context, path, contentType string) error {
	ctx.Response().Header().Set(echo.HeaderContentType, contentType)
	ctx.Response().Header().Set("Cache-Control", "public, max-age=86400")
	if err := ctx.File(path); err != nil {
		if os.IsNotExist(err) {
			return iconNotFound(ctx)
		}
		return err
	}
	return nil
}

func iconNotFound(ctx echo.Context) error {
	return ctx.JSON(http.StatusNotFound, codegen.ResponseNotFound{
		Message: utils.Ptr("icon not found"),
	})
}
