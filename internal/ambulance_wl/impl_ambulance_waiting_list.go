package ambulance_wl

import (
	"net/http"
	"time"

	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type implAmbulanceWaitingListAPI struct {
	logger zerolog.Logger
	tracer trace.Tracer
}

func NewAmbulanceWaitingListApi() AmbulanceWaitingListAPI {
	return &implAmbulanceWaitingListAPI{
		logger: log.With().Str("component", "ambulance-wl").Logger(),
		tracer: otel.Tracer("ambulance-wl"),
	}
}

func (o implAmbulanceWaitingListAPI) CreateWaitingListEntry(c *gin.Context) {
	ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
	defer span.End()
	// update request context to build span hierarchy accross calls and services
	c.Request = c.Request.WithContext(ctx)

	updateAmbulanceFunc(c, func(c *gin.Context, ambulance *Ambulance) (*Ambulance, interface{}, int) {
		ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry-updateAmbulanceFunc")
		defer span.End()
		// update context to build span hierarchy accross calls
		c.Request = c.Request.WithContext(ctx)

		logger := o.logger.With().
			Str("method", "CreateWaitingListEntry").
			Str("ambulanceId", ambulance.Id).
			Str("ambulanceName", ambulance.Name).
			Logger()
		var entry WaitingListEntry

		if err := c.ShouldBindJSON(&entry); err != nil {
			logger.Error().Err(err).Msg("Failed to bind JSON")
			span.SetStatus(codes.Error, "Failed to bind JSON")
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Invalid request body",
				"error":   err.Error(),
			}, http.StatusBadRequest
		}

		if entry.PatientId == "" {
			logger.Error().Msg("Patient ID is required")
			span.SetStatus(codes.Error, "Patient ID is required")
			logger.Trace().Msgf("Entry: %+v", entry)
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Patient ID is required",
			}, http.StatusBadRequest
		}

		if entry.Id == "" || entry.Id == "@new" {
			entry.Id = uuid.NewString()
			logger.Debug().
				Str("entry-id", entry.Id).
				Msg("Generating new ID for entry")
		}

		conflictIndx := slices.IndexFunc(ambulance.WaitingList, func(waiting WaitingListEntry) bool {
			return entry.Id == waiting.Id || entry.PatientId == waiting.PatientId
		})

		if conflictIndx >= 0 {
			logger.Error().Msg("Entry already exists")
			span.SetStatus(codes.Error, "Entry already exists")
			return nil, gin.H{
				"status":  http.StatusConflict,
				"message": "Entry already exists",
			}, http.StatusConflict
		}

		ambulance.WaitingList = append(ambulance.WaitingList, entry)
		ambulance.reconcileWaitingList()
		// entry was copied by value return reconciled value from the list
		entryIndx := slices.IndexFunc(ambulance.WaitingList, func(waiting WaitingListEntry) bool {
			return entry.Id == waiting.Id
		})
		if entryIndx < 0 {
			logger.Error().Msg("Failed to find entry in waiting list after saving")
			span.SetStatus(codes.Error, "Failed to find entry in waiting list after saving")
			return nil, gin.H{
				"status":  http.StatusInternalServerError,
				"message": "Failed to save entry",
			}, http.StatusInternalServerError
		}
		logger.Info().
			Str("patient-id", ambulance.WaitingList[entryIndx].Id).
			Msg("Succesfully created patient entry")
		span.SetStatus(codes.Ok, "Succesfully created patient entry")
		return ambulance, ambulance.WaitingList[entryIndx], http.StatusOK
	})
}

func (o implAmbulanceWaitingListAPI) DeleteWaitingListEntry(c *gin.Context) {
	updateAmbulanceFunc(c, func(c *gin.Context, ambulance *Ambulance) (*Ambulance, interface{}, int) {
		entryId := c.Param("entryId")

		if entryId == "" {
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Entry ID is required",
			}, http.StatusBadRequest
		}

		entryIndx := slices.IndexFunc(ambulance.WaitingList, func(waiting WaitingListEntry) bool {
			return entryId == waiting.Id
		})

		if entryIndx < 0 {
			return nil, gin.H{
				"status":  http.StatusNotFound,
				"message": "Entry not found",
			}, http.StatusNotFound
		}

		ambulance.WaitingList = append(ambulance.WaitingList[:entryIndx], ambulance.WaitingList[entryIndx+1:]...)
		ambulance.reconcileWaitingList()
		return ambulance, nil, http.StatusNoContent
	})
}

func (o implAmbulanceWaitingListAPI) GetWaitingListEntries(c *gin.Context) {
	ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
	defer span.End()
	// update request context to build span hierarchy accross calls and services
	c.Request = c.Request.WithContext(ctx)
	updateAmbulanceFunc(c, func(c *gin.Context, ambulance *Ambulance) (*Ambulance, interface{}, int) {
		ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
		defer span.End()
		// update request context to build span hierarchy accross calls and services
		c.Request = c.Request.WithContext(ctx)
		result := ambulance.WaitingList
		if result == nil {
			result = []WaitingListEntry{}
		}
		// return nil ambulance - no need to update it in db
		return nil, result, http.StatusOK
	})
}

func (o implAmbulanceWaitingListAPI) GetWaitingListEntry(c *gin.Context) {
	ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
	defer span.End()
	// update request context to build span hierarchy accross calls and services
	c.Request = c.Request.WithContext(ctx)
	updateAmbulanceFunc(c, func(c *gin.Context, ambulance *Ambulance) (*Ambulance, interface{}, int) {
		ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
		defer span.End()
		// update request context to build span hierarchy accross calls and services
		c.Request = c.Request.WithContext(ctx)
		entryId := c.Param("entryId")

		if entryId == "" {
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Entry ID is required",
			}, http.StatusBadRequest
		}

		entryIndx := slices.IndexFunc(ambulance.WaitingList, func(waiting WaitingListEntry) bool {
			return entryId == waiting.Id
		})

		if entryIndx < 0 {
			return nil, gin.H{
				"status":  http.StatusNotFound,
				"message": "Entry not found",
			}, http.StatusNotFound
		}

		// return nil ambulance - no need to update it in db
		return nil, ambulance.WaitingList[entryIndx], http.StatusOK
	})
}

func (o implAmbulanceWaitingListAPI) UpdateWaitingListEntry(c *gin.Context) {
	ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
	defer span.End()
	// update request context to build span hierarchy accross calls and services
	c.Request = c.Request.WithContext(ctx)
	updateAmbulanceFunc(c, func(c *gin.Context, ambulance *Ambulance) (*Ambulance, interface{}, int) {
		ctx, span := o.tracer.Start(c.Request.Context(), "CreateWaitingListEntry")
		defer span.End()
		// update request context to build span hierarchy accross calls and services
		c.Request = c.Request.WithContext(ctx)
		var entry WaitingListEntry

		if err := c.ShouldBindJSON(&entry); err != nil {
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Invalid request body",
				"error":   err.Error(),
			}, http.StatusBadRequest
		}

		entryId := c.Param("entryId")

		if entryId == "" {
			return nil, gin.H{
				"status":  http.StatusBadRequest,
				"message": "Entry ID is required",
			}, http.StatusBadRequest
		}

		entryIndx := slices.IndexFunc(ambulance.WaitingList, func(waiting WaitingListEntry) bool {
			return entryId == waiting.Id
		})

		if entryIndx < 0 {
			return nil, gin.H{
				"status":  http.StatusNotFound,
				"message": "Entry not found",
			}, http.StatusNotFound
		}

		if entry.PatientId != "" {
			ambulance.WaitingList[entryIndx].PatientId = entry.PatientId
		}

		if entry.Id != "" {
			ambulance.WaitingList[entryIndx].Id = entry.Id
		}

		if entry.WaitingSince.After(time.Time{}) {
			ambulance.WaitingList[entryIndx].WaitingSince = entry.WaitingSince
		}

		if entry.EstimatedDurationMinutes > 0 {
			ambulance.WaitingList[entryIndx].EstimatedDurationMinutes = entry.EstimatedDurationMinutes
		}

		ambulance.reconcileWaitingList()
		return ambulance, ambulance.WaitingList[entryIndx], http.StatusOK
	})
}
