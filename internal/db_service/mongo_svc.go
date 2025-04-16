package db_service

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type DbService[DocType interface{}] interface {
	CreateDocument(ctx context.Context, id string, document *DocType) error
	FindDocument(ctx context.Context, id string) (*DocType, error)
	UpdateDocument(ctx context.Context, id string, document *DocType) error
	DeleteDocument(ctx context.Context, id string) error
	Disconnect(ctx context.Context) error
}

var ErrNotFound = fmt.Errorf("document not found")
var ErrConflict = fmt.Errorf("conflict: document already exists")

type MongoServiceConfig struct {
	ServerHost string
	ServerPort int
	UserName   string
	Password   string
	DbName     string
	Collection string
	Timeout    time.Duration
}

type mongoSvc[DocType interface{}] struct {
	MongoServiceConfig
	client     atomic.Pointer[mongo.Client]
	clientLock sync.Mutex

	tracer trace.Tracer
}

func NewMongoService[DocType interface{}](config MongoServiceConfig) DbService[DocType] {
	enviro := func(name string, defaultValue string) string {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return defaultValue
	}

	svc := &mongoSvc[DocType]{}
	svc.tracer = otel.Tracer("MongoService")
	svc.MongoServiceConfig = config

	if svc.ServerHost == "" {
		svc.ServerHost = enviro("AMBULANCE_API_MONGODB_HOST", "localhost")
	}

	if svc.ServerPort == 0 {
		port := enviro("AMBULANCE_API_MONGODB_PORT", "27017")
		if port, err := strconv.Atoi(port); err == nil {
			svc.ServerPort = port
		} else {
			log.Printf("Invalid port value: %v", port)
			svc.ServerPort = 27017
		}
	}

	if svc.UserName == "" {
		svc.UserName = enviro("AMBULANCE_API_MONGODB_USERNAME", "")
	}

	if svc.Password == "" {
		svc.Password = enviro("AMBULANCE_API_MONGODB_PASSWORD", "")
	}

	if svc.DbName == "" {
		svc.DbName = enviro("AMBULANCE_API_MONGODB_DATABASE", "cv1-ambulance-wl")
	}

	if svc.Collection == "" {
		svc.Collection = enviro("AMBULANCE_API_MONGODB_COLLECTION", "ambulance")
	}

	if svc.Timeout == 0 {
		seconds := enviro("AMBULANCE_API_MONGODB_TIMEOUT_SECONDS", "10")
		if seconds, err := strconv.Atoi(seconds); err == nil {
			svc.Timeout = time.Duration(seconds) * time.Second
		} else {
			log.Printf("Invalid timeout value: %v", seconds)
			svc.Timeout = 10 * time.Second
		}
	}

	log.Printf(
		"MongoDB config: //%v@%v:%v/%v/%v",
		svc.UserName,
		svc.ServerHost,
		svc.ServerPort,
		svc.DbName,
		svc.Collection,
	)
	return svc
}

func (m *mongoSvc[DocType]) connect(ctx context.Context) (*mongo.Client, error) {
	ctx, span := m.tracer.Start(ctx, "connect")
	defer span.End()
	// optimistic check
	client := m.client.Load()
	if client != nil {
		return client, nil
	}

	m.clientLock.Lock()
	defer m.clientLock.Unlock()
	// pesimistic check
	client = m.client.Load()
	if client != nil {
		return client, nil
	}

	ctx, contextCancel := context.WithTimeout(ctx, m.Timeout)
	defer contextCancel()

	var uri = fmt.Sprintf("mongodb://%v:%v", m.ServerHost, m.ServerPort)
	span.SetAttributes(attribute.String("mongodb.uri", uri))
	log.Printf("Using URI: " + uri)

	if len(m.UserName) != 0 {
		uri = fmt.Sprintf("mongodb://%v:%v@%v:%v", m.UserName, m.Password, m.ServerHost, m.ServerPort)
	}

	opts := options.Client()
	opts.Monitor = otelmongo.NewMonitor()
	opts.ApplyURI(uri).SetConnectTimeout(10 * time.Second)
	if client, err := mongo.Connect(ctx, opts); err != nil {
		span.SetStatus(codes.Error, "MongoDB connection error")
		return nil, err
	} else {
		m.client.Store(client)
		span.SetStatus(codes.Ok, "MongoDB connection established")
		return client, nil
	}
}

func (m *mongoSvc[DocType]) Disconnect(ctx context.Context) error {
	client := m.client.Load()

	if client != nil {
		m.clientLock.Lock()
		defer m.clientLock.Unlock()

		client = m.client.Load()
		defer m.client.Store(nil)
		if client != nil {
			if err := client.Disconnect(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mongoSvc[DocType]) CreateDocument(ctx context.Context, id string, document *DocType) error {
	ctx, span := m.tracer.Start(
		ctx,
		"CreateDocument",
		trace.WithAttributes(
			attribute.String("mongodb.collection", m.Collection),
			attribute.String("entry.id", id),
		),
	)
	defer span.End()

	ctx, contextCancel := context.WithTimeout(ctx, m.Timeout)
	defer contextCancel()
	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	db := client.Database(m.DbName)
	collection := db.Collection(m.Collection)
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {

	case nil: // no error means there is conflicting document
		return ErrConflict
	case mongo.ErrNoDocuments:
		// do nothing, this is expected
	default: // other errors - return them
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}

	_, err = collection.InsertOne(ctx, document)
	span.SetStatus(codes.Ok, "Document inserted")
	return err
}

func (m *mongoSvc[DocType]) FindDocument(ctx context.Context, id string) (*DocType, error) {
	ctx, span := m.tracer.Start(
		ctx,
		"CreateDocument",
		trace.WithAttributes(
			attribute.String("mongodb.collection", m.Collection),
			attribute.String("entry.id", id),
		),
	)
	defer span.End()
	ctx, contextCancel := context.WithTimeout(ctx, m.Timeout)
	defer contextCancel()
	client, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	db := client.Database(m.DbName)
	collection := db.Collection(m.Collection)
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
	case mongo.ErrNoDocuments:
		span.SetStatus(codes.Error, "Document not found")
		return nil, ErrNotFound
	default: // other errors - return them
		return nil, result.Err()
	}
	var document *DocType
	if err := result.Decode(&document); err != nil {
		span.SetStatus(codes.Error, "Document decode error")
		return nil, err
	}
	span.SetStatus(codes.Ok, "Document found")
	return document, nil
}

func (m *mongoSvc[DocType]) UpdateDocument(ctx context.Context, id string, document *DocType) error {
	ctx, span := m.tracer.Start(
		ctx,
		"CreateDocument",
		trace.WithAttributes(
			attribute.String("mongodb.collection", m.Collection),
			attribute.String("entry.id", id),
		),
	)
	defer span.End()

	ctx, contextCancel := context.WithTimeout(ctx, m.Timeout)
	defer contextCancel()

	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	db := client.Database(m.DbName)
	collection := db.Collection(m.Collection)
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
	case mongo.ErrNoDocuments:
		span.SetStatus(codes.Error, "Document not found")
		return ErrNotFound
	default: // other errors - return them
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}
	_, err = collection.ReplaceOne(ctx, bson.D{{Key: "id", Value: id}}, document)
	return err
}

func (m *mongoSvc[DocType]) DeleteDocument(ctx context.Context, id string) error {
	ctx, contextCancel := context.WithTimeout(ctx, m.Timeout)
	defer contextCancel()
	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	db := client.Database(m.DbName)
	collection := db.Collection(m.Collection)
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
	case mongo.ErrNoDocuments:
		return ErrNotFound
	default: // other errors - return them
		return result.Err()
	}
	_, err = collection.DeleteOne(ctx, bson.D{{Key: "id", Value: id}})
	return err
}
