package service

import (
	"github.com/sjgoldie/go-restgen/datastore"
)

// New creates and returns a Common service instance for type T
func New[T any]() (*Common[T], error) {
	ds, err := datastore.Get()
	if err != nil {
		return nil, err
	}

	return &Common[T]{
		store: &datastore.Wrapper[T]{
			Store: ds,
		},
	}, nil
}
