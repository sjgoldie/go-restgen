// Package handler provides generic HTTP handlers for RESTful CRUD endpoints.
//
// By default, handler warnings are suppressed (log level set to Error).
// To enable warnings in development, set the log level:
//
//	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
//	    Level: slog.LevelWarn,
//	}))
//	slog.SetDefault(logger)
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strconv"

	"github.com/go-chi/chi/v5"
	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

func init() {
	// Set default log level to Error (suppresses warnings in production)
	// Users can override this in their main() before starting handlers
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(logger)
}

// GetAll handles GET requests to retrieve all items of type T
func GetAll[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Extract relations from context if available
		ctx := r.Context()
		relations, ok := ctx.Value("relations").([]string)
		if !ok {
			relations = []string{}
		}

		items, err := svc.GetAll(r.Context(), relations)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to get all items", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(items); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Get handles GET requests to retrieve a single item of type T by ID
func Get[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Extract relations from context if available
		ctx := r.Context()
		relations, ok := ctx.Value("relations").([]string)
		if !ok {
			relations = []string{}
		}

		// Get metadata to find the correct URL parameter UUID
		meta, err := metadata.Get[T]()
		if err != nil {
			slog.Warn("metadata not found for type", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		item, err := svc.Get(r.Context(), id, relations)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to get item", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(item); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Create handles POST requests to create a new item of type T
func Create[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		savedItem, err := svc.Create(r.Context(), item)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrDuplicate) {
				http.Error(w, "resource already exists", http.StatusBadRequest)
				return
			}
			if errors.Is(err, apperrors.ErrInvalidReference) {
				http.Error(w, "invalid reference to related resource", http.StatusBadRequest)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to create item", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Update handles PUT requests to update an existing item of type T
func Update[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata to find the correct URL parameter UUID
		meta, err := metadata.Get[T]()
		if err != nil {
			slog.Warn("metadata not found for type", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var item T
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			slog.Warn("failed to decode request body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Set ID from path onto the struct (overwrite any ID from JSON)
		reflect.ValueOf(&item).Elem().FieldByName("ID").SetInt(int64(id))

		savedItem, err := svc.Update(r.Context(), id, item)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, apperrors.ErrInvalidReference) {
				http.Error(w, "invalid reference to related resource", http.StatusBadRequest)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to update item", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(savedItem); err != nil {
			slog.Warn("failed to encode response", "error", err)
		}
	}
}

// Delete handles DELETE requests to delete an item of type T by ID
func Delete[T any]() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := service.New[T]()
		if err != nil {
			slog.Warn("failed to create service", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Get metadata to find the correct URL parameter UUID
		meta, err := metadata.Get[T]()
		if err != nil {
			slog.Warn("metadata not found for type", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Parse ID from URL parameter
		id, err := strconv.Atoi(chi.URLParam(r, meta.URLParamUUID))
		if err != nil {
			slog.Warn("invalid id parameter", "error", err, "paramUUID", meta.URLParamUUID)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if err := svc.Delete(r.Context(), id); err != nil {
			if errors.Is(err, context.Canceled) {
				return // Client disconnected, no response needed
			}
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
				return
			}
			if errors.Is(err, apperrors.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, apperrors.ErrUnavailable) {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			slog.Warn("failed to delete item", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
