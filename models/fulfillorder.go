package models

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
	"strconv"

	"github.com/Microsoft/ApplicationInsights-Go/appinsights"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/matryer/try.v1"
)

var (
	database string
	password string
	status   string
)

var db string

var mongoHost = os.Getenv("MONGOHOST")
var mongoUsername = os.Getenv("MONGOUSER")
var mongoPassword = os.Getenv("MONGOPASSWORD")
var mongoSSL = false
var mongoPort = ""
var teamName = os.Getenv("TEAMNAME")
var isCosmosDb = strings.Contains(mongoHost, "documents.azure.com")

// MongoDB database and collection names
var mongoDatabaseName = "akschallenge"
var mongoCollectionName = "orders"
var mongoDBSession *mgo.Session
var mongoDBSessionError error
var mongoPoolLimit = 25

// Application Insights telemetry clients
var ChallengeTelemetryClient appinsights.TelemetryClient
var CustomTelemetryClient appinsights.TelemetryClient

// ProcessOrder represents the process order json
type ProcessOrder struct {
	OrderID string `json:"orderId"`
}

// Order represents the order json
type Order struct {
	ID           			bson.ObjectId		`json:"id" bson:"_id,omitempty"`
	EmailAddress      string  				`json:"emailAddress"`
	Product           string  				`json:"product"`
	Total             float64 				`json:"total"`
	Status            string  				`json:"status"`
}

func init() {

	// Let's validate and spool the ENV VARS
	// Validate environment variables
	validateVariable(mongoHost, "MONGOHOST")
	validateVariable(mongoUsername, "MONGOUSERNAME")
	validateVariable(mongoPassword, "MONGOPASSWORD")
	validateVariable(teamName, "TEAMNAME")

	var mongoPoolLimitEnv = os.Getenv("MONGOPOOL_LIMIT")
	if mongoPoolLimitEnv != "" {
		if limit, err := strconv.Atoi(mongoPoolLimitEnv); err == nil {
			mongoPoolLimit = limit
		}
	}
	log.Printf("MongoDB pool limit set to %v. You can override by setting the MONGOPOOL_LIMIT environment variable." , mongoPoolLimit)

	if isCosmosDb {
		log.Println("Using CosmosDB")
		db = "CosmosDB"
		mongoSSL = true
		mongoPort = ":10255"

	} else {
		log.Println("Using MongoDB")
		db = "MongoDB"
		mongoSSL = false
		mongoPort = ""
	}

	// Parse the connection string to extract components because the MongoDB driver is peculiar
	var dialInfo *mgo.DialInfo
	
	mongoDatabase := mongoDatabaseName // can be anything

	log.Printf("\tUsername: %s", mongoUsername)
	log.Printf("\tPassword: %s", mongoPassword)
	log.Printf("\tHost: %s", mongoHost)
	log.Printf("\tPort: %s", mongoPort)
	log.Printf("\tDatabase: %s", mongoDatabase)
	log.Printf("\tSSL: %t", mongoSSL)

	if mongoSSL {
		dialInfo = &mgo.DialInfo{
			Addrs:    []string{mongoHost+mongoPort},
			Timeout:  60 * time.Second,
			Database: mongoDatabase, // It can be anything
			Username: mongoUsername, // Username
			Password: mongoPassword, // Password
			DialServer: func(addr *mgo.ServerAddr) (net.Conn, error) {
				return tls.Dial("tcp", addr.String(), &tls.Config{})
			},
		}
	} else {
		dialInfo = &mgo.DialInfo{
			Addrs:    []string{mongoHost+mongoPort},
			Timeout:  60 * time.Second,
			Database: mongoDatabase, // It can be anything
			Username: mongoUsername, // Username
			Password: mongoPassword, // Password
		}
	}

	success := false
	mongoDBSession, mongoDBSessionError = mgo.DialWithInfo(dialInfo)
	if mongoDBSessionError != nil {
		log.Fatal(fmt.Sprintf("Can't connect to mongo at [%s], go error: ", mongoHost+mongoPort), mongoDBSessionError)
		trackException(mongoDBSessionError)
	} else {
		success = true
	}

	if !success {
		os.Exit(1)
	}

	// SetSafe changes the session safety mode.
	// If the safe parameter is nil, the session is put in unsafe mode, and writes become fire-and-forget,
	// without error checking. The unsafe mode is faster since operations won't hold on waiting for a confirmation.
	// http://godoc.org/labix.org/v2/mgo#Session.SetMode.
	mongoDBSession.SetSafe(nil)

	mongoDBSession.SetMode(mgo.Monotonic, true)

	// Limit connection pool to avoid running into Request Rate Too Large on CosmosDB
	mongoDBSession.SetPoolLimit(mongoPoolLimit)
}

func ProcessOrderInMongoDB(orderID string) bool {
	log.Println("ProcessOrderInMongoDB: " + orderID)

	mongoDBSessionCopy := mongoDBSession.Copy()
	defer mongoDBSessionCopy.Close()

	// Get collection
	log.Println("Getting collection: " + mongoCollectionName + " in database: " + mongoDatabaseName)
	mongoDBCollection := mongoDBSessionCopy.DB(mongoDatabaseName).C(mongoCollectionName)

	// Get Document from collection
	result := Order{}

	// Unserialize OrderIDHex to BSON ObjectId
	orderIDObjectID := bson.ObjectIdHex(orderID)
	log.Println("Looking for ", "{", "_id:", orderIDObjectID, ",", "status:", "Open", "}")

	err := mongoDBCollection.Find(bson.M{"_id": orderIDObjectID, "status": "Open"}).One(&result)

	if err != nil {
		log.Println("Not found (already processed) or error: ", err)
		if err.Error() == "not found" {
			return true // no need to keep it around - return true so that it gets removed from the service bus
		}
	} else {
		change := bson.M{"$set": bson.M{"status": "Processed"}}

		// Try updating the record, with retry logic
		err := try.Do(func(attempt int) (bool, error) {
			var err error
		
			err = mongoDBCollection.Update(result, change)
	
			if err != nil {
				log.Println("Error processing record. Will retry in 3 seconds:", err)
				trackException(err)
				time.Sleep(3 * time.Second) // wait
			} else {
				log.Println("set status: Processed")
			}
			return attempt < 3, err
		  })
		  
		if err != nil {
			log.Println("Error updating record after retrying 3 times: ", err)
			return false
		}
	}

	// Track the event for the challenge purposes
	eventTelemetry := appinsights.NewEventTelemetry("FulfillOrder db " + db)
	eventTelemetry.Properties["team"] = teamName
	eventTelemetry.Properties["sequence"] = "4"
	eventTelemetry.Properties["type"] = db
	eventTelemetry.Properties["service"] = "FulfillOrder"
	eventTelemetry.Properties["orderId"] = orderID
	trackEvent(eventTelemetry)

	return true
}

func WriteToFileSystem(orderID string) bool {
	// Let's place on the file system
	log.Println("Attempting to write order to file share")
	f, err := os.Create("/orders/" + orderID + ".json")
	if err != nil {
		log.Println("Couldn't write order to file share")		
		trackException(err)
		return false
	}

	// Issue a `Sync` to flush writes to stable storage.
	err = f.Sync()
	if err != nil {
		log.Println(err)
		trackException(err)
		return false
	} else {
		eventTelemetry := appinsights.NewEventTelemetry("FulfillOrder fileshare")
		eventTelemetry.Properties["team"] = teamName
		eventTelemetry.Properties["sequence"] = "5"
		eventTelemetry.Properties["orderId"] = orderID
		eventTelemetry.Properties["type"] = "fileshare"
		eventTelemetry.Properties["service"] = "FulfillOrder"
		trackEvent(eventTelemetry)
	}

	fmt.Fprintf(f, "{", "orderid:", orderID, ",", "status:", "Processed", "}")

	return true
}

// Logs out value of a variable
func validateVariable(value string, envName string) {
	if len(value) == 0 {
		log.Printf("The environment variable %s has not been set", envName)
	} else {
		log.Printf("The environment variable %s is %s", envName, value)
	}
}

func trackEvent(eventTelemetry *appinsights.EventTelemetry) {
		ChallengeTelemetryClient.Track(eventTelemetry)
		if CustomTelemetryClient != nil {
			CustomTelemetryClient.Track(eventTelemetry)
		}
}

func trackException(err error) {
	if err != nil {
		log.Println(err)
		ChallengeTelemetryClient.TrackException(err)
		if CustomTelemetryClient != nil {
			CustomTelemetryClient.TrackException(err)
		}
	}
}