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
func (s *Common[T]) GetAll(ctx context.Context, relations []string) ([]*T, error) {
	return s.store.GetAll(ctx, relations)
}

// Get retrieves a single item of type T by ID
func (s *Common[T]) Get(ctx context.Context, id int, relations []string) (*T, error) {
	return s.store.Get(ctx, id, relations)
}

// Create creates a new item of type T
func (s *Common[T]) Create(ctx context.Context, item T) (*T, error) {
	return s.store.Create(ctx, item)
}

// Update updates an existing item of type T
func (s *Common[T]) Update(ctx context.Context, id int, item T) (*T, error) {
	return s.store.Update(ctx, id, item)
}

// Delete deletes an item of type T by ID
func (s *Common[T]) Delete(ctx context.Context, id int) error {
	return s.store.Delete(ctx, id)
}
