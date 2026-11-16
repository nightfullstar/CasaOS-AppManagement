package v2

import (
	"net/http"
	"os"

	"github.com/ReCasaOS/CasaOS-AppManagement/codegen"
	"github.com/ReCasaOS/CasaOS-AppManagement/service"
	"github.com/ReCasaOS/CasaOS-Common/utils"
	"github.com/labstack/echo/v4"
)

func (a *AppManagement) AppStoreAppIcon(ctx echo.Context, id codegen.AppStoreID, app codegen.AppStoreAppID, _ codegen.AppStoreAppIconParams) error {
	if service.MyService == nil {
		return iconNotFound(ctx)
	}
	path, contentType, err := service.MyService.AppStoreManagement().IconFile(id, app)
	if err != nil {
		return iconNotFound(ctx)
	}
	return sendAppStoreAsset(ctx, path, contentType, "icon not found")
}

func (a *AppManagement) AppStoreAppScreenshot(ctx echo.Context, id codegen.AppStoreID, app codegen.AppStoreAppID, file codegen.AppStoreScreenshotFile, _ codegen.AppStoreAppScreenshotParams) error {
	if service.MyService == nil {
		return screenshotNotFound(ctx)
	}
	path, contentType, err := service.MyService.AppStoreManagement().ScreenshotFile(id, app, file)
	if err != nil {
		return screenshotNotFound(ctx)
	}
	return sendAppStoreAsset(ctx, path, contentType, "screenshot not found")
}

func serveAppStoreIcon(ctx echo.Context, workdir, app string) error {
	path, contentType, err := service.FindStoreAppIcon(workdir, app)
	if err != nil {
		return iconNotFound(ctx)
	}
	return sendAppStoreAsset(ctx, path, contentType, "icon not found")
}

func sendAppStoreAsset(ctx echo.Context, path, contentType, missing string) error {
	ctx.Response().Header().Set(echo.HeaderContentType, contentType)
	ctx.Response().Header().Set("Cache-Control", "public, max-age=86400")
	if err := ctx.File(path); err != nil {
		if os.IsNotExist(err) {
			return storeAssetNotFound(ctx, missing)
		}
		return err
	}
	return nil
}

func iconNotFound(ctx echo.Context) error {
	return storeAssetNotFound(ctx, "icon not found")
}

func screenshotNotFound(ctx echo.Context) error {
	return storeAssetNotFound(ctx, "screenshot not found")
}

func storeAssetNotFound(ctx echo.Context, message string) error {
	return ctx.JSON(http.StatusNotFound, codegen.ResponseNotFound{
		Message: utils.Ptr(message),
	})
}
