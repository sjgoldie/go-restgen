package router

import (
	"reflect"
	"slices"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
)

type partitionInternalModel struct {
	bun.BaseModel `bun:"table:partition_internal_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Region        string `bun:"region"`
	Channel       string `bun:"channel"`
}

type partitionInternalProject struct {
	bun.BaseModel `bun:"table:partition_internal_projects"`
	ID            int    `bun:"id,pk,autoincrement"`
	Region        string `bun:"region"`
}

type partitionInternalTask struct {
	bun.BaseModel `bun:"table:partition_internal_tasks"`
	ID            int                       `bun:"id,pk,autoincrement"`
	ProjectID     int                       `bun:"project_id"`
	Project       *partitionInternalProject `bun:"rel:belongs-to,join:project_id=id"`
	Notes         []*partitionInternalNote  `bun:"rel:has-many,join:id=task_id"`
}

type partitionInternalNote struct {
	bun.BaseModel `bun:"table:partition_internal_notes"`
	ID            int                    `bun:"id,pk,autoincrement"`
	TaskID        int                    `bun:"task_id"`
	Task          *partitionInternalTask `bun:"rel:belongs-to,join:task_id=id"`
}

func TestResolvePartitions(t *testing.T) {
	modelType := reflect.TypeFor[partitionInternalModel]()

	t.Run("root route uses its own declarations", func(t *testing.T) {
		got := resolvePartitions(nil, modelType, "ID", []PartitionConfig{WithPartition("region", "Region")})
		want := []metadata.Partition{{Name: "region", Field: "Region"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("child inherits parent partitions without a field", func(t *testing.T) {
		parent := &metadata.TypeMetadata{Partitions: []metadata.Partition{{Name: "region", Field: "Region"}}}
		got := resolvePartitions(parent, modelType, "ID", nil)
		want := []metadata.Partition{{Name: "region"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("child overrides an inherited partition and adds its own", func(t *testing.T) {
		parent := &metadata.TypeMetadata{Partitions: []metadata.Partition{{Name: "region", Field: "Region"}}}
		got := resolvePartitions(parent, modelType, "ID", []PartitionConfig{
			WithPartition("region", "Region"),
			WithPartition("channel", "Channel"),
		})
		want := []metadata.Partition{{Name: "region", Field: "Region"}, {Name: "channel", Field: "Channel"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("invalid declarations are kept so access stays closed", func(t *testing.T) {
		got := resolvePartitions(nil, modelType, "ID", []PartitionConfig{
			WithPartition("", "Region"),
			WithPartition("channel", "NoSuchField"),
			WithPartition("project", "ID"),
		})
		want := []metadata.Partition{{Name: "", Field: "Region"}, {Name: "channel", Field: "NoSuchField"}, {Name: "project", Field: "ID"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("relation path resolves to the related model's field", func(t *testing.T) {
		got := resolvePartitions(nil, reflect.TypeFor[partitionInternalNote](), "ID", []PartitionConfig{WithPartition("region", "Task.Project.Region")})
		want := []metadata.Partition{{
			Name:  "region",
			Field: "Region",
			Via: []metadata.RelationStep{
				{Name: "Task", ModelType: reflect.TypeFor[partitionInternalTask](), Table: "partition_internal_tasks", FKColumn: "task_id", JoinColumn: "id", PKColumn: "id"},
				{Name: "Project", ModelType: reflect.TypeFor[partitionInternalProject](), Table: "partition_internal_projects", FKColumn: "project_id", JoinColumn: "id", PKColumn: "id"},
			},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("invalid relation paths are kept unresolved", func(t *testing.T) {
		taskType := reflect.TypeFor[partitionInternalTask]()
		cases := map[string]string{
			"unknown relation":                   "Owner.Region",
			"has-many relation":                  "Notes.TaskID",
			"unknown field on the related model": "Project.Missing",
			"primary key of the related model":   "Project.ID",
		}
		for name, field := range cases {
			got := resolvePartitions(nil, taskType, "ID", []PartitionConfig{WithPartition("region", field)})
			want := []metadata.Partition{{Name: "region", Field: field}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got %+v, want %+v", name, got, want)
			}
		}
	})
}

func TestResolvePartitionAccess(t *testing.T) {
	partitions := []metadata.Partition{{Name: "region", Field: "Region"}}

	t.Run("global scope is unrestricted", func(t *testing.T) {
		access, ok := resolvePartitionAccess(&AuthInfo{Scopes: []string{"read"}}, []string{"read"}, partitions)
		if !ok || !access["region"].Unrestricted {
			t.Errorf("expected unrestricted access, got %+v, %v", access, ok)
		}
	})

	t.Run("grants for required scopes are merged without duplicates", func(t *testing.T) {
		authInfo := &AuthInfo{Grants: []ScopedGrant{
			{Scope: "read", Partition: "region", Values: []string{"emea", "apac"}},
			{Scope: "admin", Partition: "region", Values: []string{"apac", "amer"}},
			{Scope: "write", Partition: "region", Values: []string{"anz"}},
		}}
		access, ok := resolvePartitionAccess(authInfo, []string{"read", "admin"}, partitions)
		if !ok {
			t.Fatal("expected access")
		}
		if got := access["region"]; got.Unrestricted || !slices.Equal(got.Values, []string{"emea", "apac", "amer"}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a partition without a grant denies access", func(t *testing.T) {
		authInfo := &AuthInfo{Grants: []ScopedGrant{{Scope: "read", Partition: "region", Values: []string{"emea"}}}}
		two := append(slices.Clone(partitions), metadata.Partition{Name: "channel", Field: "Channel"})
		if _, ok := resolvePartitionAccess(authInfo, []string{"read"}, two); ok {
			t.Error("expected access to be denied")
		}
	})

	t.Run("a grant with no values grants nothing but is not unrestricted", func(t *testing.T) {
		authInfo := &AuthInfo{Grants: []ScopedGrant{{Scope: "read", Partition: "region"}}}
		access, ok := resolvePartitionAccess(authInfo, []string{"read"}, partitions)
		if !ok {
			t.Fatal("expected the grant to count")
		}
		if got := access["region"]; got.Unrestricted || len(got.Values) != 0 {
			t.Errorf("got %+v", got)
		}
	})
}

func TestCheckAuth_Partitioned(t *testing.T) {
	partitions := []metadata.Partition{{Name: "region", Field: "Region"}}
	grants := []ScopedGrant{{Scope: "read", Partition: "region", Values: []string{"emea"}}}
	share := &ShareConfig{Model: (*partitionInternalModel)(nil)}

	tests := []struct {
		name      string
		authInfo  *AuthInfo
		config    AuthConfig
		want      authStatus
		ownership bool
	}{
		{"no auth", nil, AuthConfig{Scopes: []string{"read"}}, authUnauthorized, false},
		{"public config", &AuthInfo{UserID: "u", Scopes: []string{"read"}}, AllPublic(), authForbidden, false},
		{"auth-only config", &AuthInfo{UserID: "u", Scopes: []string{"read"}}, IsAuthenticated(), authForbidden, false},
		{"scope-less ownership config", &AuthInfo{UserID: "u", Scopes: []string{"read"}}, AllWithOwnershipUnless([]string{"OwnerID"}), authForbidden, false},
		{"ownership without a user ID", &AuthInfo{Grants: grants}, AuthConfig{Scopes: []string{"read"}, Ownership: &OwnershipConfig{Fields: []string{"OwnerID"}}}, authUnauthorized, false},
		{"share without a user ID", &AuthInfo{Grants: grants}, AuthConfig{Scopes: []string{"read"}, Share: share}, authUnauthorized, false},
		{"scoped grant", &AuthInfo{UserID: "u", Grants: grants}, AuthConfig{Scopes: []string{"read"}}, authOK, false},
		{"scoped grant with ownership", &AuthInfo{UserID: "u", Grants: grants}, AuthConfig{Scopes: []string{"read"}, Ownership: &OwnershipConfig{Fields: []string{"OwnerID"}}}, authOK, true},
		{"no matching grant", &AuthInfo{UserID: "u", Grants: grants}, AuthConfig{Scopes: []string{"write"}}, authForbidden, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkAuth(tt.authInfo, &tt.config, partitions)
			if result.Status != tt.want {
				t.Errorf("status: got %v, want %v", result.Status, tt.want)
			}
			if result.ApplyOwnership != tt.ownership {
				t.Errorf("ownership: got %v, want %v", result.ApplyOwnership, tt.ownership)
			}
			if tt.want == authOK && result.Partitions == nil {
				t.Error("expected partition access on success")
			}
		})
	}

	t.Run("unpartitioned types ignore grants", func(t *testing.T) {
		result := checkAuth(&AuthInfo{UserID: "u", Grants: grants}, &AuthConfig{Scopes: []string{"read"}}, nil)
		if result.Status != authForbidden {
			t.Errorf("got %v, want forbidden", result.Status)
		}
	})

	t.Run("unpartitioned share config requires a user ID", func(t *testing.T) {
		result := checkAuth(&AuthInfo{Scopes: []string{"read"}}, &AuthConfig{Scopes: []string{"read"}, Share: share}, nil)
		if result.Status != authUnauthorized {
			t.Errorf("got %v, want unauthorized", result.Status)
		}
	})
}
