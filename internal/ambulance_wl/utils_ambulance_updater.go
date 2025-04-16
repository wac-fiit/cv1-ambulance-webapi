package ambulance_wl

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wac-fiit/cv1-ambulance-webapi/internal/db_service"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

type ambulanceUpdater = func(
	ctx *gin.Context,
	ambulance *Ambulance,
) (updatedAmbulance *Ambulance, responseContent interface{}, status int)

func updateAmbulanceFunc(ctx *gin.Context, updater ambulanceUpdater) {
	tracer := otel.Tracer("ambulance-wl")
	spanCtx, span := tracer.Start(ctx.Request.Context(), "updateAmbulanceFunc")
	defer span.End()

	ctx.Request = ctx.Request.WithContext(spanCtx)

	value, exists := ctx.Get("db_service")
	if !exists {
		span.SetStatus(codes.Error, "db_service not found")
		ctx.JSON(
			http.StatusInternalServerError,
			gin.H{
				"status":  "Internal Server Error",
				"message": "db_service not found",
				"error":   "db_service not found",
			})
		return
	}

	db, ok := value.(db_service.DbService[Ambulance])
	if !ok {
		span.SetStatus(codes.Error, "db_service context is not of type db_service.DbService")
		ctx.JSON(
			http.StatusInternalServerError,
			gin.H{
				"status":  "Internal Server Error",
				"message": "db_service context is not of type db_service.DbService",
				"error":   "cannot cast db_service context to db_service.DbService",
			})
		return
	}

	ambulanceId := ctx.Param("ambulanceId")
	ambulance, err := db.FindDocument(ctx.Request.Context(), ambulanceId)

	switch err {
	case nil:
		// continue
	case db_service.ErrNotFound:
		span.SetStatus(codes.Error, "Ambulance not found")
		ctx.JSON(
			http.StatusNotFound,
			gin.H{
				"status":  "Not Found",
				"message": "Ambulance not found",
				"error":   err.Error(),
			},
		)
		return
	default:
		span.SetStatus(codes.Error, "Failed to load ambulance from database")
		ctx.JSON(
			http.StatusBadGateway,
			gin.H{
				"status":  "Bad Gateway",
				"message": "Failed to load ambulance from database",
				"error":   err.Error(),
			})
		return
	}

	if !ok {
		span.SetStatus(codes.Error, "Failed to cast ambulance from database")
		ctx.JSON(
			http.StatusInternalServerError,
			gin.H{
				"status":  "Internal Server Error",
				"message": "Failed to cast ambulance from database",
				"error":   "Failed to cast ambulance from database",
			})
		return
	}

	updatedAmbulance, responseObject, status := updater(ctx, ambulance)

	if updatedAmbulance != nil {
		err = db.UpdateDocument(ctx.Request.Context(), ambulanceId, updatedAmbulance)
	} else {
		err = nil // redundant but for clarity
	}

	switch err {
	case nil:
		span.SetStatus(codes.Ok, "Ambulance updated")
		if responseObject != nil {
			ctx.JSON(status, responseObject)
		} else {
			ctx.AbortWithStatus(status)
		}
	case db_service.ErrNotFound:
		span.SetStatus(codes.Error, "Ambulance not found")
		ctx.JSON(
			http.StatusNotFound,
			gin.H{
				"status":  "Not Found",
				"message": "Ambulance was deleted while processing the request",
				"error":   err.Error(),
			},
		)
	default:
		span.SetStatus(codes.Error, "Failed to update ambulance in database")
		ctx.JSON(
			http.StatusBadGateway,
			gin.H{
				"status":  "Bad Gateway",
				"message": "Failed to update ambulance in database",
				"error":   err.Error(),
			})
	}

}
