package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/newrelic/go-agent/v3/integrations/nrmongo"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

var (
	collection *mongo.Collection
	Log        *zap.Logger
	err        error
	app        *newrelic.Application
)

// make mongoURI point to TELEMETRY cloud mongo and change the requestURL in TELEMETRY for this server

type Event struct {
	ID             primitive.ObjectID     `json:"id" bson:"_id"`
	InstallationID string                 `json:"installationId" bson:"installation_id,omitempty"`
	EventType      string                 `json:"eventType" bson:"event_type,omitempty"`
	Meta           map[string]interface{} `json:"meta" bson:"meta"`
	CreatedAt      int64                  `json:"createdAt" bson:"created_at"`
	StoredAt       int64                  `bson:"stored_at"`
}

func (e *Event) Bind(r *http.Request) error {
	if e.EventType == "" {
		return errors.New("EventType cant be empty")
	}
	return nil
}

func main() {
	Log, err = zap.NewProduction()
	defer func() {
		_ = Log.Sync() // flushes buffer, if any
	}()
	port := "3030"

	// Set client options
	clientOptions := options.Client().ApplyURI(os.Getenv("TELEMETRY_MONGO_URI"))
	// Connect to MongoDB
	m := nrmongo.NewCommandMonitor(nil)
	client, err := mongo.Connect(context.TODO(), clientOptions.SetMonitor(m))
	if err != nil {
		Log.Error("failed to connect to mongo", zap.Error(err))
	}
	// Check the connection
	nrCTX := newrelic.NewContext(context.Background(), nil)
	err = client.Ping(nrCTX, nil)
	if err != nil {
		Log.Error("failed to connect to mongo", zap.Error(err))
	}

	fmt.Println("Connected to MongoDB!")
	collection = client.Database("TELEMETRY-telemetry").Collection("telemetry")

	r := chi.NewRouter()
	app, err = newrelic.NewApplication(
		newrelic.ConfigAppName("ping-analytics"),
		newrelic.ConfigLicense("0***************************L"),
		newrelic.ConfigDistributedTracerEnabled(true),
	)
	if err != nil {
		Log.Error("failed to connect to mongo", zap.Error(err))
	}
	r.Use(middleware.Logger)

	// r.Post("/analytics", handler)
	r.Post(newrelic.WrapHandleFunc(app, "/analytics", handler))
	// r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
	// 	w.Write([]byte("welcome"))
	// })
	r.Get(newrelic.WrapHandleFunc(app, "/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("welcome"))
	}))

	fmt.Printf("Listening on port :%v\n", port)
	http.ListenAndServe(":"+port, r)
}

func handler(w http.ResponseWriter, r *http.Request) {
	data := &Event{}
	if err := render.Bind(r, data); err != nil {
		Log.Error("error parsing request", zap.Error(err))
		render.Render(w, r, ErrInvalidRequest(400, err))
		return
	}

	id := primitive.NewObjectID()

	// for the first time the installationId is generated and returned to store
	if data.InstallationID == "" {
		data.InstallationID = id.String()
		data.ID = id
	} else {
		data.ID = id
	}
	data.StoredAt = time.Now().Unix()
	// nrCTX := newrelic.NewContext(r.Context(), nil)
	_, err := collection.InsertOne(r.Context(), data)
	if err != nil {
		Log.Error("failed to insert analytics", zap.Error(err))
		render.Render(w, r, ErrInvalidRequest(400, err))
	}

	respBody := map[string]interface{}{
		"message":        "Captured analytics",
		"InstallationID": data.InstallationID,
	}
	bin, err := json.Marshal(respBody)
	if err != nil {
		Log.Error("failed to marshal the resp", zap.Error(err))
		render.Render(w, r, ErrInvalidRequest(400, err))
	}
	// defer txn.End()
	w.Write(bin)
}

func ErrInvalidRequest(status int, err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: status,
		StatusText:     "Invalid request.",
		ErrorText:      err.Error(),
	}
}

func (e *ErrResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}

type ErrResponse struct {
	Err            error `json:"-"` // low-level runtime error
	HTTPStatusCode int   `json:"-"` // http response status code

	StatusText string `json:"status"`          // ddb-level status message
	AppCode    int64  `json:"code,omitempty"`  // application-specific error code
	ErrorText  string `json:"error,omitempty"` // application-level error message, for debugging
}
