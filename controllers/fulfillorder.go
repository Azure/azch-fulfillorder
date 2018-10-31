package controllers

import (
	"encoding/json"

	"fulfillorderack/models"

	"github.com/astaxie/beego"
)

// Operations about order
type OrderController struct {
	beego.Controller
}

// @Title Process Order
// @Description process order POST
// @Param	body	body 	models.Order true		"body for order content"
// @Success 200 {string} models.Order.ID
// @Failure 403 body is empty
// @router / [post]
func (this *OrderController) Post() {
	var ob models.Order
	json.Unmarshal(this.Ctx.Input.RequestBody, &ob)
	orderID := models.ProcessOrderInMongoDB(ob)
	this.Data["json"] = map[string]string{"orderId": orderID}
	this.ServeJSON()
}
