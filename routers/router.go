// @APIVersion 1.0.0
// @Title Fulfillorder
// @Description beego has a very cool tools to autogenerate documents for your API
// @Contact shanepeckham@live.com
// @TermsOfServiceUrl http://beego.me/
// @License Apache 2.0
// @LicenseUrl http://www.apache.org/licenses/LICENSE-2.0.html
package routers

import (
	"fulfillorderack/controllers"

	"github.com/astaxie/beego"
	"github.com/astaxie/beego/context"
)

func init() {
	ns := beego.NewNamespace("/v1",
		beego.NSNamespace("/order",
			beego.NSInclude(
				&controllers.OrderController{},
			),
		),
	)
	beego.AddNamespace(ns)
	beego.Get("/healthz", func(ctx *context.Context) {
		ctx.Output.Body([]byte("i'm alive!"))
	})
}
