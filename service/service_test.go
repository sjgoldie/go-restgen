package service_test

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/service"

	"github.com/uptrace/bun"
)

// TestModel is a simple test model
type TestModel struct {
	bun.BaseModel `bun:"table:test_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
	Email         string `bun:"email,unique"`
}

var testDB *datastore.SQLite

func TestMain(m *testing.M) {
	// Setup
	var err error
	testDB, err = datastore.NewSQLite(":memory:")
	if err != nil {
		panic("Failed to create test database: " + err.Error())
	}

	if err := datastore.Initialize(testDB); err != nil {
		testDB.Cleanup()
		panic("Failed to initialize datastore: " + err.Error())
	}

	// Register metadata for TestModel
	meta := &metadata.TypeMetadata{
		TypeID:        metadata.GenerateTypeID(),
		TypeName:      "TestModel",
		TableName:     "test_models",
		URLParamUUID:  "id",
		ParentType:    nil,
		ForeignKeyCol: "",
	}
	metadata.Register(meta, reflect.TypeOf(TestModel{}))

	_, err = testDB.GetDB().NewCreateTable().Model((*TestModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		testDB.Cleanup()
		panic("Failed to create table: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Teardown
	testDB.GetDB().NewDropTable().Model((*TestModel)(nil)).IfExists().Exec(context.Background())
	datastore.Cleanup()
	testDB.Cleanup()

	os.Exit(code)
}

func cleanTable(t *testing.T) {
	t.Helper()
	db, _ := datastore.Get()
	_, err := db.GetDB().NewDelete().Model((*TestModel)(nil)).Where("1=1").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to clean table:", err)
	}
	_, _ = db.GetDB().Exec("DELETE FROM sqlite_sequence WHERE name = 'test_models'")
}

func TestService_New(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "successful service creation",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := service.New[TestModel]()
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && svc == nil {
				t.Error("Expected service to be created, got nil")
			}
		})
	}
}

func TestService_GetAll(t *testing.T) {
	tests := []struct {
		name          string
		setupData     []TestModel
		expectedCount int
	}{
		{
			name:          "empty table",
			setupData:     []TestModel{},
			expectedCount: 0,
		},
		{
			name: "multiple items",
			setupData: []TestModel{
				{Name: "Item 1", Email: "item1@example.com"},
				{Name: "Item 2", Email: "item2@example.com"},
				{Name: "Item 3", Email: "item3@example.com"},
			},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			db, _ := datastore.Get()
			for _, item := range tt.setupData {
				_, err := db.GetDB().NewInsert().Model(&item).Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test GetAll
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			items, err := svc.GetAll(context.Background(), []string{})
			if err != nil {
				t.Fatal("GetAll failed:", err)
			}

			if len(items) != tt.expectedCount {
				t.Errorf("Expected %d items, got %d", tt.expectedCount, len(items))
			}
		})
	}
}

func TestService_Get(t *testing.T) {
	tests := []struct {
		name         string
		setupItem    *TestModel
		getID        int
		wantErr      bool
		expectedName string
	}{
		{
			name:         "existing item",
			setupItem:    &TestModel{Name: "Test Item", Email: "test@example.com"},
			getID:        1,
			wantErr:      false,
			expectedName: "Test Item",
		},
		{
			name:      "not found",
			setupItem: nil,
			getID:     999,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Get
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			item, err := svc.Get(context.Background(), tt.getID, []string{})
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && item.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, item.Name)
			}
		})
	}
}

func TestService_Create(t *testing.T) {
	tests := []struct {
		name      string
		item      TestModel
		wantErr   bool
		checkName string
	}{
		{
			name:      "valid item",
			item:      TestModel{Name: "New Item", Email: "new@example.com"},
			wantErr:   false,
			checkName: "New Item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			created, err := svc.Create(context.Background(), tt.item)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if created.ID == 0 {
					t.Error("Expected ID to be set")
				}
				if created.Name != tt.checkName {
					t.Errorf("Expected name '%s', got '%s'", tt.checkName, created.Name)
				}
			}
		})
	}
}

func TestService_Update(t *testing.T) {
	tests := []struct {
		name         string
		setupItem    *TestModel
		updateItem   TestModel
		wantErr      bool
		expectedName string
	}{
		{
			name:         "valid update",
			setupItem:    &TestModel{Name: "Original", Email: "original@example.com"},
			updateItem:   TestModel{ID: 1, Name: "Updated", Email: "updated@example.com"},
			wantErr:      false,
			expectedName: "Updated",
		},
		{
			name:       "not found",
			setupItem:  nil,
			updateItem: TestModel{ID: 999, Name: "Name", Email: "email@example.com"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Update
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			updated, err := svc.Update(context.Background(), tt.updateItem.ID, tt.updateItem)
			if (err != nil) != tt.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && updated.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, updated.Name)
			}
		})
	}
}

func TestService_Delete(t *testing.T) {
	tests := []struct {
		name      string
		setupItem *TestModel
		deleteID  int
		wantErr   bool
	}{
		{
			name:      "valid delete",
			setupItem: &TestModel{Name: "To Delete", Email: "delete@example.com"},
			deleteID:  1,
			wantErr:   false,
		},
		{
			name:      "not found",
			setupItem: nil,
			deleteID:  999,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTable(t)

			// Setup data
			if tt.setupItem != nil {
				db, _ := datastore.Get()
				_, err := db.GetDB().NewInsert().Model(tt.setupItem).Returning("*").Exec(context.Background())
				if err != nil {
					t.Fatal("Failed to insert test item:", err)
				}
			}

			// Test Delete
			svc, err := service.New[TestModel]()
			if err != nil {
				t.Fatal("Failed to create service:", err)
			}

			err = svc.Delete(context.Background(), tt.deleteID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify deletion if successful
			if !tt.wantErr {
				db, _ := datastore.Get()
				var check TestModel
				err := db.GetDB().NewSelect().Model(&check).Where("id = ?", tt.deleteID).Scan(context.Background())
				if err == nil {
					t.Error("Expected item to be deleted")
				}
			}
		})
	}
}

func TestService_TypeSafety(t *testing.T) {
	type User struct {
		bun.BaseModel `bun:"table:users"`
		ID            int    `bun:"id,pk,autoincrement"`
		Name          string `bun:"name"`
	}

	type Product struct {
		bun.BaseModel `bun:"table:products"`
		ID            int     `bun:"id,pk,autoincrement"`
		Price         float64 `bun:"price"`
	}

	userSvc, err := service.New[User]()
	if err != nil {
		t.Fatal("Failed to get user service:", err)
	}

	productSvc, err := service.New[Product]()
	if err != nil {
		t.Fatal("Failed to get product service:", err)
	}

	if userSvc == nil || productSvc == nil {
		t.Fatal("Failed to create type-safe services")
	}
}
