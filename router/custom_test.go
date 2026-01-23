package router

import (
	"context"
	"io"
	"testing"

	"github.com/sjgoldie/go-restgen/filestore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"
)

// Test model for custom handler tests
type CustomTestModel struct {
	ID   int    `bun:"id,pk"`
	Name string `bun:"name"`
}

func TestWithCustomGet(t *testing.T) {
	customFunc := func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*CustomTestModel, error) {
		return &CustomTestModel{ID: 1, Name: "test"}, nil
	}

	config := WithCustomGet(customFunc)

	if config.Fn == nil {
		t.Error("Expected Fn to be set")
	}

	// Verify it returns the expected type
	var _ = config
}

func TestWithCustomGetAll(t *testing.T) {
	customFunc := func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*CustomTestModel, int, map[string]float64, error) {
		return []*CustomTestModel{{ID: 1, Name: "test"}}, 1, nil, nil
	}

	config := WithCustomGetAll(customFunc)

	if config.Fn == nil {
		t.Error("Expected Fn to be set")
	}

	var _ = config
}

func TestWithCustomCreate(t *testing.T) {
	customFunc := func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item CustomTestModel, _ io.Reader, _ filestore.FileMetadata) (*CustomTestModel, error) {
		return &item, nil
	}

	config := WithCustomCreate(customFunc)

	if config.Fn == nil {
		t.Error("Expected Fn to be set")
	}

	var _ = config
}

func TestWithCustomUpdate(t *testing.T) {
	customFunc := func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item CustomTestModel) (*CustomTestModel, error) {
		return &item, nil
	}

	config := WithCustomUpdate(customFunc)

	if config.Fn == nil {
		t.Error("Expected Fn to be set")
	}

	var _ = config
}

func TestWithCustomDelete(t *testing.T) {
	customFunc := func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
		return nil
	}

	config := WithCustomDelete(customFunc)

	if config.Fn == nil {
		t.Error("Expected Fn to be set")
	}

	var _ = config
}

// Test that custom configs work with RegisterRoutes type switch
func TestCustomConfigTypesInSwitch(t *testing.T) {
	// Create various config types
	getConfig := WithCustomGet(func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*CustomTestModel, error) {
		return nil, nil
	})

	getAllConfig := WithCustomGetAll(func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*CustomTestModel, int, map[string]float64, error) {
		return nil, 0, nil, nil
	})

	createConfig := WithCustomCreate(func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item CustomTestModel, _ io.Reader, _ filestore.FileMetadata) (*CustomTestModel, error) {
		return nil, nil
	})

	updateConfig := WithCustomUpdate(func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item CustomTestModel) (*CustomTestModel, error) {
		return nil, nil
	})

	deleteConfig := WithCustomDelete(func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
		return nil
	})

	// Test type assertions work correctly
	options := []interface{}{getConfig, getAllConfig, createConfig, updateConfig, deleteConfig}

	var custom customHandlers[CustomTestModel]
	for _, opt := range options {
		switch v := opt.(type) {
		case CustomGetConfig[CustomTestModel]:
			custom.get = v.Fn
		case CustomGetAllConfig[CustomTestModel]:
			custom.getAll = v.Fn
		case CustomCreateConfig[CustomTestModel]:
			custom.create = v.Fn
		case CustomUpdateConfig[CustomTestModel]:
			custom.update = v.Fn
		case CustomDeleteConfig[CustomTestModel]:
			custom.delete = v.Fn
		}
	}

	if custom.get == nil {
		t.Error("Expected get to be set")
	}
	if custom.getAll == nil {
		t.Error("Expected getAll to be set")
	}
	if custom.create == nil {
		t.Error("Expected create to be set")
	}
	if custom.update == nil {
		t.Error("Expected update to be set")
	}
	if custom.delete == nil {
		t.Error("Expected delete to be set")
	}
}

// Verify the func types match handler package types
func TestCustomFuncTypesMatchHandler(t *testing.T) {
	// These should compile if the types match correctly
	var _ handler.CustomGetFunc[CustomTestModel] = func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) (*CustomTestModel, error) {
		return nil, nil
	}

	var _ handler.CustomGetAllFunc[CustomTestModel] = func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo) ([]*CustomTestModel, int, map[string]float64, error) {
		return nil, 0, nil, nil
	}

	var _ handler.CustomCreateFunc[CustomTestModel] = func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, item CustomTestModel, _ io.Reader, _ filestore.FileMetadata) (*CustomTestModel, error) {
		return nil, nil
	}

	var _ handler.CustomUpdateFunc[CustomTestModel] = func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string, item CustomTestModel) (*CustomTestModel, error) {
		return nil, nil
	}

	var _ handler.CustomDeleteFunc[CustomTestModel] = func(ctx context.Context, svc *service.Common[CustomTestModel], meta *metadata.TypeMetadata, auth *metadata.AuthInfo, id string) error {
		return nil
	}
}
