package controllers

import (
	"encoding/json"
	"fulfillorderack/models"
	"os"
	"time"
	"fmt"
	"github.com/astaxie/beego"
	"github.com/Microsoft/ApplicationInsights-Go/appinsights"
)

var customInsightsKey = os.Getenv("APPINSIGHTS_KEY")
var challengeInsightsKey = os.Getenv("CHALLENGEAPPINSIGHTS_KEY")
var teamName = os.Getenv("TEAMNAME")

// Application Insights telemetry clients
var challengeTelemetryClient appinsights.TelemetryClient
var customTelemetryClient appinsights.TelemetryClient

// Operations about order
type OrderController struct {
	beego.Controller
}

func init() {
	// Init App Insights
	challengeTelemetryClient = appinsights.NewTelemetryClient(challengeInsightsKey)
	challengeTelemetryClient.Context().Tags.Cloud().SetRole("fulfillorder_golang")

	if customInsightsKey != "" {
		customTelemetryClient = appinsights.NewTelemetryClient(customInsightsKey)

		// Set role instance name globally -- this is usually the
		// name of the service submitting the telemetry
		customTelemetryClient.Context().Tags.Cloud().SetRole("fulfillorder_golang")
	}
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

	// Inject telemetry clients
	models.CustomTelemetryClient = customTelemetryClient;
	models.ChallengeTelemetryClient = challengeTelemetryClient;

	// Track the request
	requestStartTime := time.Now()

	processedInMongoDB := models.ProcessOrderInMongoDB(ob)
	writtenToFileSystem := models.WriteToFileSystem(ob.OrderID)

	trackRequest(requestStartTime, time.Now(), processedInMongoDB && writtenToFileSystem)

	this.Data["json"] = map[string]string{"orderId": ob.OrderID, "processedInMongoDB": fmt.Sprint(processedInMongoDB), "writtenToFileSystem": fmt.Sprint(writtenToFileSystem)}
	this.ServeJSON()
}

func trackRequest(requestStartTime time.Time, requestEndTime time.Time, requestSuccess bool) {
	var responseCode = "200"
	if requestSuccess != true {
		responseCode = "500"
	} 
	requestTelemetry := appinsights.NewRequestTelemetry("POST", "fulfillorders/orders/v1", 0, responseCode)
	requestTelemetry.MarkTime(requestStartTime, requestEndTime)
	requestTelemetry.Properties["team"] = teamName
	requestTelemetry.Properties["service"] = "FulfillOrder"

	challengeTelemetryClient.Track(requestTelemetry)
	if customTelemetryClient != nil {
		customTelemetryClient.Track(requestTelemetry)
	}
}
