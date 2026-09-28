package app

import (
	"github.com/labstack/echo/v4"
)

func echoForTest(api *API) *echo.Echo {
	e := echo.New()
	e.GET("/private", func(c echo.Context) error { return c.NoContent(204) }, api.requireAuth)
	return e
}
