package main

import (
	//"log"
	"os"
	"strings"

	"context"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/wac-fiit/cv1-ambulance-webapi/api"
	"github.com/wac-fiit/cv1-ambulance-webapi/internal/ambulance_wl"
	"github.com/wac-fiit/cv1-ambulance-webapi/internal/db_service"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: zerolog.TimeFormatUnix}
	log.Logger = zerolog.New(output).With().
		Str("service", "ambulance-wl-list").
		Timestamp().
		Caller().
		Logger()

	logLevelStr := os.Getenv("LOG_LEVEL")
	defaultLevel := zerolog.InfoLevel
	level, err := zerolog.ParseLevel(strings.ToLower(logLevelStr))
	if err != nil {
		log.Warn().Str("LOG_LEVEL", logLevelStr).Msgf("Invalid log level, using default: %s", defaultLevel)
		level = defaultLevel
	}
	// Set the global log level
	zerolog.SetGlobalLevel(level)
	// initialize trace exporter
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	traceExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize trace exporter")
	}

	traceProvider := tracesdk.NewTracerProvider(tracesdk.WithBatcher(traceExporter))
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer traceProvider.Shutdown(ctx)

	// initialize metric exporter
	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize metric reader")
	}
	metricProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(metricReader))
	otel.SetMeterProvider(metricProvider)
	defer metricProvider.Shutdown(ctx)

	log.Info().Msg("Server started")
	//log.Printf("Server started")
	port := os.Getenv("AMBULANCE_API_PORT")
	if port == "" {
		port = "8080"
	}
	environment := os.Getenv("AMBULANCE_API_ENVIRONMENT")
	if !strings.EqualFold(environment, "production") { // case insensitive comparison
		gin.SetMode(gin.DebugMode)
	}
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(otelgin.Middleware("ambulance-webapi"))
	corsMiddleware := cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "PUT", "POST", "DELETE", "PATCH"},
		AllowHeaders:     []string{"Origin", "Authorization", "Content-Type"},
		ExposeHeaders:    []string{""},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	})
	engine.Use(corsMiddleware)

	// setup context update  middleware
	dbService := db_service.NewMongoService[ambulance_wl.Ambulance](db_service.MongoServiceConfig{})
	defer dbService.Disconnect(context.Background())
	engine.Use(func(ctx *gin.Context) {
		ctx.Set("db_service", dbService)
		ctx.Next()
	})
	// request routings
	handleFunctions := &ambulance_wl.ApiHandleFunctions{
		AmbulanceConditionsAPI:  ambulance_wl.NewAmbulanceConditionsApi(),
		AmbulanceWaitingListAPI: ambulance_wl.NewAmbulanceWaitingListApi(),
		AmbulancesAPI:           ambulance_wl.NewAmbulancesApi(),
	}
	ambulance_wl.NewRouterWithGinEngine(engine, *handleFunctions)
	engine.GET("/openapi", api.HandleOpenApi)
	engine.Run(":" + port)
}
