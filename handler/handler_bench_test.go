//nolint:staticcheck,errcheck,gosec,maintidx,goconst // Benchmark code - acceptable for test files
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sjgoldie/go-restgen/datastore"
	"github.com/sjgoldie/go-restgen/handler"
	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/uptrace/bun"
)

// ============================================================================
// Benchmark Models - 4-level nested hierarchy
// ============================================================================

// BenchBlog - Level 1 (root)
type BenchBlog struct {
	bun.BaseModel `bun:"table:bench_blogs"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	OwnerID       string       `bun:"owner_id,notnull" json:"owner_id"`
	Name          string       `bun:"name,notnull" json:"name"`
	CreatedAt     time.Time    `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	Posts         []*BenchPost `bun:"rel:has-many,join:id=blog_id" json:"-"`
}

// BenchPost - Level 2 (nested under Blog)
type BenchPost struct {
	bun.BaseModel `bun:"table:bench_posts"`
	ID            int             `bun:"id,pk,autoincrement" json:"id"`
	BlogID        int             `bun:"blog_id,notnull,skipupdate" json:"blog_id"`
	Blog          *BenchBlog      `bun:"rel:belongs-to,join:blog_id=id" json:"-"`
	OwnerID       string          `bun:"owner_id,notnull" json:"owner_id"`
	Title         string          `bun:"title,notnull" json:"title"`
	Content       string          `bun:"content" json:"content"`
	CreatedAt     time.Time       `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	Comments      []*BenchComment `bun:"rel:has-many,join:id=post_id" json:"-"`
}

// BenchComment - Level 3 (nested under Post)
type BenchComment struct {
	bun.BaseModel `bun:"table:bench_comments"`
	ID            int              `bun:"id,pk,autoincrement" json:"id"`
	PostID        int              `bun:"post_id,notnull,skipupdate" json:"post_id"`
	Post          *BenchPost       `bun:"rel:belongs-to,join:post_id=id" json:"-"`
	Text          string           `bun:"text,notnull" json:"text"`
	Author        string           `bun:"author,notnull" json:"author"`
	CreatedAt     time.Time        `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
	Reactions     []*BenchReaction `bun:"rel:has-many,join:id=comment_id" json:"-"`
}

// BenchReaction - Level 4 (nested under Comment)
type BenchReaction struct {
	bun.BaseModel `bun:"table:bench_reactions"`
	ID            int           `bun:"id,pk,autoincrement" json:"id"`
	CommentID     int           `bun:"comment_id,notnull,skipupdate" json:"comment_id"`
	Comment       *BenchComment `bun:"rel:belongs-to,join:comment_id=id" json:"-"`
	Emoji         string        `bun:"emoji,notnull" json:"emoji"`
	UserID        string        `bun:"user_id,notnull" json:"user_id"`
	CreatedAt     time.Time     `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
}

// ============================================================================
// Benchmark Metadata
// ============================================================================

// Metadata for benchmark types - used for context injection
var benchBlogMeta = &metadata.TypeMetadata{
	TypeID:        "bench_blog_id",
	TypeName:      "BenchBlog",
	TableName:     "bench_blogs",
	URLParamUUID:  "blogId",
	ModelType:     reflect.TypeOf(BenchBlog{}),
	ParentType:    nil,
	ForeignKeyCol: "",
}

var benchPostMeta = &metadata.TypeMetadata{
	TypeID:        "bench_post_id",
	TypeName:      "BenchPost",
	TableName:     "bench_posts",
	URLParamUUID:  "postId",
	ModelType:     reflect.TypeOf(BenchPost{}),
	ParentType:    reflect.TypeOf(BenchBlog{}),
	ParentMeta:    benchBlogMeta,
	ForeignKeyCol: "blog_id",
}

var benchCommentMeta = &metadata.TypeMetadata{
	TypeID:        "bench_comment_id",
	TypeName:      "BenchComment",
	TableName:     "bench_comments",
	URLParamUUID:  "commentId",
	ModelType:     reflect.TypeOf(BenchComment{}),
	ParentType:    reflect.TypeOf(BenchPost{}),
	ParentMeta:    benchPostMeta,
	ForeignKeyCol: "post_id",
}

var benchReactionMeta = &metadata.TypeMetadata{
	TypeID:        "bench_reaction_id",
	TypeName:      "BenchReaction",
	TableName:     "bench_reactions",
	URLParamUUID:  "reactionId",
	ModelType:     reflect.TypeOf(BenchReaction{}),
	ParentType:    reflect.TypeOf(BenchComment{}),
	ParentMeta:    benchCommentMeta,
	ForeignKeyCol: "comment_id",
}

// addMetaToRequest adds metadata to request context
func addMetaToRequest(r *http.Request, meta *metadata.TypeMetadata) *http.Request {
	ctx := context.WithValue(r.Context(), metadata.MetadataKey, meta)
	return r.WithContext(ctx)
}

// ============================================================================
// Benchmark Database Setup
// ============================================================================

// benchInitialized tracks whether benchmark tables have been created
var benchInitialized bool

// setupBenchDB ensures benchmark tables exist in the test database
func setupBenchDB() error {
	if benchInitialized {
		return nil
	}

	// Use the global datastore that was initialized in TestMain
	db, err := datastore.Get()
	if err != nil {
		return fmt.Errorf("failed to get datastore: %w", err)
	}

	ctx := context.Background()

	// Create benchmark tables in the existing test database
	models := []interface{}{
		(*BenchBlog)(nil),
		(*BenchPost)(nil),
		(*BenchComment)(nil),
		(*BenchReaction)(nil),
	}

	for _, model := range models {
		_, err := db.GetDB().NewCreateTable().Model(model).IfNotExists().Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	benchInitialized = true
	return nil
}

// cleanupBenchDB removes all data from benchmark tables
func cleanupBenchDB(b *testing.B) {
	db, err := datastore.Get()
	if err != nil {
		b.Fatal("failed to get datastore:", err)
	}

	ctx := context.Background()
	db.GetDB().NewDelete().Model((*BenchReaction)(nil)).Where("1=1").Exec(ctx)
	db.GetDB().NewDelete().Model((*BenchComment)(nil)).Where("1=1").Exec(ctx)
	db.GetDB().NewDelete().Model((*BenchPost)(nil)).Where("1=1").Exec(ctx)
	db.GetDB().NewDelete().Model((*BenchBlog)(nil)).Where("1=1").Exec(ctx)
}

// ============================================================================
// Test Data Fixtures
// ============================================================================

// seedBenchData creates test data with specified quantities
func seedBenchData(b *testing.B, numBlogs, numPostsPerBlog, numCommentsPerPost, numReactionsPerComment int) ([]int, []int, []int, []int) {
	ds, err := datastore.Get()
	if err != nil {
		b.Fatal("failed to get datastore:", err)
	}

	ctx := context.Background()
	db := ds.GetDB()

	blogIDs := make([]int, numBlogs)
	postIDs := []int{}
	commentIDs := []int{}
	reactionIDs := []int{}

	// Create blogs
	for i := 0; i < numBlogs; i++ {
		blog := &BenchBlog{
			OwnerID:   fmt.Sprintf("user-%d", i%3), // Rotate between 3 users
			Name:      fmt.Sprintf("Benchmark Blog %d", i),
			CreatedAt: time.Now(),
		}
		_, err := db.NewInsert().Model(blog).Exec(ctx)
		if err != nil {
			b.Fatal(err)
		}
		blogIDs[i] = blog.ID

		// Create posts for this blog
		for j := 0; j < numPostsPerBlog; j++ {
			post := &BenchPost{
				BlogID:    blog.ID,
				OwnerID:   fmt.Sprintf("user-%d", i%3),
				Title:     fmt.Sprintf("Post %d-%d", i, j),
				Content:   fmt.Sprintf("Content for post %d-%d", i, j),
				CreatedAt: time.Now(),
			}
			_, err := db.NewInsert().Model(post).Exec(ctx)
			if err != nil {
				b.Fatal(err)
			}
			postIDs = append(postIDs, post.ID)

			// Create comments for this post
			for k := 0; k < numCommentsPerPost; k++ {
				comment := &BenchComment{
					PostID:    post.ID,
					Text:      fmt.Sprintf("Comment %d-%d-%d", i, j, k),
					Author:    fmt.Sprintf("Author %d", k%5),
					CreatedAt: time.Now(),
				}
				_, err := db.NewInsert().Model(comment).Exec(ctx)
				if err != nil {
					b.Fatal(err)
				}
				commentIDs = append(commentIDs, comment.ID)

				// Create reactions for this comment
				for l := 0; l < numReactionsPerComment; l++ {
					reaction := &BenchReaction{
						CommentID: comment.ID,
						Emoji:     []string{"👍", "❤️", "😂", "🎉", "🚀"}[l%5],
						UserID:    fmt.Sprintf("user-%d", l%10),
						CreatedAt: time.Now(),
					}
					_, err := db.NewInsert().Model(reaction).Exec(ctx)
					if err != nil {
						b.Fatal(err)
					}
					reactionIDs = append(reactionIDs, reaction.ID)
				}
			}
		}
	}

	return blogIDs, postIDs, commentIDs, reactionIDs
}

// ============================================================================
// Auth Test Helpers
// ============================================================================

// authScenario represents an authentication test case
type authScenario struct {
	name        string
	userID      string
	scopes      []string
	shouldPass  bool
	description string
}

// Note: These scenarios test handler performance with different auth contexts.
// Actual auth enforcement happens in middleware (not benchmarked here).
var authScenarios = []authScenario{
	{
		name:        "NoAuth",
		userID:      "",
		scopes:      nil,
		shouldPass:  true,
		description: "No authentication context (handler passes, middleware would block)",
	},
	{
		name:        "WithAuth",
		userID:      "user-0",
		scopes:      []string{"user"},
		shouldPass:  true,
		description: "With auth context populated",
	},
	{
		name:        "WithOwnership",
		userID:      "user-0",
		scopes:      []string{"user"},
		shouldPass:  true,
		description: "With ownership context (tests ownership filtering overhead)",
	},
	{
		name:        "AdminBypass",
		userID:      "admin-1",
		scopes:      []string{"user", "admin"},
		shouldPass:  true,
		description: "Admin with bypass scope (tests bypass code path)",
	},
}

// addAuthToRequest adds authentication context to a request
func addAuthToRequest(r *http.Request, userID string, scopes []string) *http.Request {
	if userID == "" {
		return r
	}

	authInfo := &router.AuthInfo{
		UserID: userID,
		Scopes: scopes,
	}
	ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
	return r.WithContext(ctx)
}

// ============================================================================
// Timing Infrastructure
// ============================================================================

// TimingResult captures timing breakdown for a request
type TimingResult struct {
	Total         time.Duration
	HandlerStart  time.Time
	ServiceStart  time.Time
	ServiceEnd    time.Time
	DatastoreTime time.Duration
	HandlerEnd    time.Time
}

// String returns a formatted timing breakdown
func (t *TimingResult) String() string {
	return fmt.Sprintf("Total: %v | Handler: %v | Service: %v | Datastore: %v",
		t.Total,
		t.HandlerEnd.Sub(t.HandlerStart),
		t.ServiceEnd.Sub(t.ServiceStart),
		t.DatastoreTime,
	)
}

// ============================================================================
// Benchmark: Nested Depth Tests
// ============================================================================

// BenchmarkNestedDepth tests performance at different nesting levels
func BenchmarkNestedDepth(b *testing.B) {
	if err := setupBenchDB(); err != nil {
		b.Fatal(err)
	}

	tests := []struct {
		name  string
		path  string
		depth int
		setup func(b *testing.B) (blogID, postID, commentID, reactionID int)
	}{
		{
			name:  "Depth1_Blog",
			path:  "/blogs/%d",
			depth: 1,
			setup: func(b *testing.B) (int, int, int, int) {
				cleanupBenchDB(b)
				blogIDs, _, _, _ := seedBenchData(b, 1, 0, 0, 0)
				return blogIDs[0], 0, 0, 0
			},
		},
		{
			name:  "Depth2_Post",
			path:  "/blogs/%d/posts/%d",
			depth: 2,
			setup: func(b *testing.B) (int, int, int, int) {
				cleanupBenchDB(b)
				blogIDs, postIDs, _, _ := seedBenchData(b, 1, 1, 0, 0)
				return blogIDs[0], postIDs[0], 0, 0
			},
		},
		{
			name:  "Depth3_Comment",
			path:  "/blogs/%d/posts/%d/comments/%d",
			depth: 3,
			setup: func(b *testing.B) (int, int, int, int) {
				cleanupBenchDB(b)
				blogIDs, postIDs, commentIDs, _ := seedBenchData(b, 1, 1, 1, 0)
				return blogIDs[0], postIDs[0], commentIDs[0], 0
			},
		},
		{
			name:  "Depth4_Reaction",
			path:  "/blogs/%d/posts/%d/comments/%d/reactions/%d",
			depth: 4,
			setup: func(b *testing.B) (int, int, int, int) {
				cleanupBenchDB(b)
				blogIDs, postIDs, commentIDs, reactionIDs := seedBenchData(b, 1, 1, 1, 1)
				return blogIDs[0], postIDs[0], commentIDs[0], reactionIDs[0]
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			blogID, postID, commentID, reactionID := tt.setup(b)

			// Build path and select handler/metadata based on depth
			var path string
			var h http.HandlerFunc
			var meta *metadata.TypeMetadata
			switch tt.depth {
			case 1:
				path = fmt.Sprintf(tt.path, blogID)
				h = handler.Get[BenchBlog]()
				meta = benchBlogMeta
			case 2:
				path = fmt.Sprintf(tt.path, blogID, postID)
				h = handler.Get[BenchPost]()
				meta = benchPostMeta
			case 3:
				path = fmt.Sprintf(tt.path, blogID, postID, commentID)
				h = handler.Get[BenchComment]()
				meta = benchCommentMeta
			case 4:
				path = fmt.Sprintf(tt.path, blogID, postID, commentID, reactionID)
				h = handler.Get[BenchReaction]()
				meta = benchReactionMeta
			}

			// Create request with auth
			req := httptest.NewRequest("GET", path, nil)
			req = addAuthToRequest(req, "user-0", []string{"user"})
			req = addMetaToRequest(req, meta)

			// Add URL params to context
			rctx := chi.NewRouteContext()
			switch tt.depth {
			case 1:
				rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
			case 2:
				rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
				rctx.URLParams.Add("postId", fmt.Sprintf("%d", postID))
			case 3:
				rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
				rctx.URLParams.Add("postId", fmt.Sprintf("%d", postID))
				rctx.URLParams.Add("commentId", fmt.Sprintf("%d", commentID))
			case 4:
				rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
				rctx.URLParams.Add("postId", fmt.Sprintf("%d", postID))
				rctx.URLParams.Add("commentId", fmt.Sprintf("%d", commentID))
				rctx.URLParams.Add("reactionId", fmt.Sprintf("%d", reactionID))
			}
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				h(w, req)

				if w.Code != http.StatusOK {
					b.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
				}
			}
		})
	}
}

// ============================================================================
// Benchmark: Auth Permutations
// ============================================================================

// BenchmarkAuthPermutations tests different authentication scenarios
func BenchmarkAuthPermutations(b *testing.B) {
	if err := setupBenchDB(); err != nil {
		b.Fatal(err)
	}

	// Setup: Create a blog owned by user-0
	cleanupBenchDB(b)
	blogIDs, _, _, _ := seedBenchData(b, 1, 0, 0, 0)
	blogID := blogIDs[0]

	for _, scenario := range authScenarios {
		b.Run(scenario.name, func(b *testing.B) {
			// Setup handler
			h := handler.Get[BenchBlog]()

			// Create request with scenario auth and metadata
			req := httptest.NewRequest("GET", fmt.Sprintf("/blogs/%d", blogID), nil)
			req = addAuthToRequest(req, scenario.userID, scenario.scopes)
			req = addMetaToRequest(req, benchBlogMeta)

			// Add URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			// Add ownership context only for the "WithOwnership" scenario
			if scenario.name == "WithOwnership" {
				ctx := req.Context()
				ctx = context.WithValue(ctx, "ownershipEnforced", true)
				ctx = context.WithValue(ctx, "ownershipUserID", scenario.userID)
				ctx = context.WithValue(ctx, "ownershipFields", []string{"OwnerID"})
				req = req.WithContext(ctx)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				h(w, req)

				// Verify expected outcome
				if scenario.shouldPass && w.Code != http.StatusOK {
					b.Fatalf("Expected success but got %d: %s", w.Code, w.Body.String())
				}
				if !scenario.shouldPass && w.Code == http.StatusOK {
					b.Fatalf("Expected failure but got success")
				}
			}
		})
	}
}

// ============================================================================
// Benchmark: Operation Types (CRUD)
// ============================================================================

// BenchmarkOperations tests different CRUD operations
func BenchmarkOperations(b *testing.B) {
	if err := setupBenchDB(); err != nil {
		b.Fatal(err)
	}

	tests := []struct {
		name   string
		method string
		setup  func(b *testing.B) (path string, body []byte)
		verify func(b *testing.B, code int, body []byte)
	}{
		{
			name:   "GetAll_Empty",
			method: "GET",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				return "/blogs", nil
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusOK {
					b.Fatalf("Expected 200, got %d", code)
				}
			},
		},
		{
			name:   "GetAll_100Items",
			method: "GET",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				seedBenchData(b, 100, 0, 0, 0)
				return "/blogs", nil
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusOK {
					b.Fatalf("Expected 200, got %d", code)
				}
			},
		},
		{
			name:   "GetAll_1000Items",
			method: "GET",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				seedBenchData(b, 1000, 0, 0, 0)
				return "/blogs", nil
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusOK {
					b.Fatalf("Expected 200, got %d", code)
				}
			},
		},
		{
			name:   "Get_Single",
			method: "GET",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				blogIDs, _, _, _ := seedBenchData(b, 1, 0, 0, 0)
				return fmt.Sprintf("/blogs/%d", blogIDs[0]), nil
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusOK {
					b.Fatalf("Expected 200, got %d", code)
				}
			},
		},
		{
			name:   "Post_Create",
			method: "POST",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				blog := BenchBlog{
					Name:    "New Benchmark Blog",
					OwnerID: "user-0",
				}
				body, _ := json.Marshal(blog)
				return "/blogs", body
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusCreated {
					b.Fatalf("Expected 201, got %d: %s", code, string(body))
				}
			},
		},
		{
			name:   "Put_Update",
			method: "PUT",
			setup: func(b *testing.B) (string, []byte) {
				cleanupBenchDB(b)
				blogIDs, _, _, _ := seedBenchData(b, 1, 0, 0, 0)
				blog := BenchBlog{
					ID:      blogIDs[0],
					Name:    "Updated Blog",
					OwnerID: "user-0",
				}
				body, _ := json.Marshal(blog)
				return fmt.Sprintf("/blogs/%d", blogIDs[0]), body
			},
			verify: func(b *testing.B, code int, body []byte) {
				if code != http.StatusOK {
					b.Fatalf("Expected 200, got %d: %s", code, string(body))
				}
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				path, body := tt.setup(b)

				var req *http.Request
				if body != nil {
					req = httptest.NewRequest(tt.method, path, bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
				} else {
					req = httptest.NewRequest(tt.method, path, nil)
				}

				req = addAuthToRequest(req, "user-0", []string{"user"})
				req = addMetaToRequest(req, benchBlogMeta)

				// Add URL params if needed
				if tt.method == "GET" || tt.method == "PUT" {
					rctx := chi.NewRouteContext()
					if len(path) > 7 && path[:7] == "/blogs/" {
						// Extract ID from path
						var blogID string
						fmt.Sscanf(path, "/blogs/%s", &blogID)
						if blogID != "" {
							rctx.URLParams.Add("blogId", blogID)
						}
					}
					req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
				}

				w := httptest.NewRecorder()
				b.StartTimer()

				switch tt.method {
				case "GET":
					if len(path) > 7 && path[:7] == "/blogs/" && path[7:] != "" {
						handler.Get[BenchBlog]()(w, req)
					} else {
						handler.GetAll[BenchBlog]()(w, req)
					}
				case "POST":
					handler.Create[BenchBlog]()(w, req)
				case "PUT":
					handler.Update[BenchBlog]()(w, req)
				}

				b.StopTimer()
				tt.verify(b, w.Code, w.Body.Bytes())
			}
		})
	}
}

// ============================================================================
// Benchmark: Nested Collections
// ============================================================================

// BenchmarkNestedCollections tests GetAll on nested resources with varying data sizes
func BenchmarkNestedCollections(b *testing.B) {
	if err := setupBenchDB(); err != nil {
		b.Fatal(err)
	}

	tests := []struct {
		name     string
		numPosts int
	}{
		{"Posts_10", 10},
		{"Posts_100", 100},
		{"Posts_1000", 1000},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			cleanupBenchDB(b)
			blogIDs, _, _, _ := seedBenchData(b, 1, tt.numPosts, 0, 0)
			blogID := blogIDs[0]

			h := handler.GetAll[BenchPost]()

			req := httptest.NewRequest("GET", fmt.Sprintf("/blogs/%d/posts", blogID), nil)
			req = addAuthToRequest(req, "user-0", []string{"user"})
			req = addMetaToRequest(req, benchPostMeta)

			// Add parent ID to context
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("blogId", fmt.Sprintf("%d", blogID))
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				h(w, req)

				if w.Code != http.StatusOK {
					b.Fatalf("Expected 200, got %d", w.Code)
				}
			}
		})
	}
}
