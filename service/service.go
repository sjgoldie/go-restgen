// Package service provides business logic layer for CRUD operations.
package service

import (
	"context"

	"github.com/sjgoldie/go-restgen/datastore"
)

// Common provides generic CRUD operations for any model type
type Common[T any] struct {
	store *datastore.Wrapper[T]
}

// GetAll retrieves all items of type T
// Returns items, total count (0 if not requested), and error
func (s *Common[T]) GetAll(ctx context.Context) ([]*T, int, error) {
	return s.store.GetAll(ctx)
}

// Get retrieves a single item of type T by ID
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Get(ctx context.Context, id string) (*T, error) {
	return s.store.Get(ctx, id)
}

// Create creates a new item of type T
func (s *Common[T]) Create(ctx context.Context, item T) (*T, error) {
	return s.store.Create(ctx, item)
}

// Update updates an existing item of type T
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Update(ctx context.Context, id string, item T) (*T, error) {
	return s.store.Update(ctx, id, item)
}

// Delete deletes an item of type T by ID
// The id parameter is a string to support both integer and UUID primary keys
func (s *Common[T]) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}
