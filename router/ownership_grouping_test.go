package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/router"
)

// Issue #144: multi-field ownership conditions must be grouped so they cannot split the
// surrounding parent, tenant, filter, and primary key conditions.

type GroupBlog struct {
	bun.BaseModel `bun:"table:group_blogs"`
	ID            int          `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      string       `bun:"author_id" json:"author_id"`
	Name          string       `bun:"name" json:"name"`
	Posts         []*GroupPost `bun:"rel:has-many,join:id=blog_id" json:"posts,omitempty"`
}

type GroupPost struct {
	bun.BaseModel `bun:"table:group_posts"`
	ID            int        `bun:"id,pk,autoincrement" json:"id"`
	BlogID        int        `bun:"blog_id" json:"blog_id"`
	Blog          *GroupBlog `bun:"rel:belongs-to,join:blog_id=id" json:"blog,omitempty"`
	AuthorID      string     `bun:"author_id" json:"author_id"`
	EditorID      string     `bun:"editor_id" json:"editor_id"`
	Title         string     `bun:"title" json:"title"`
	Status        string     `bun:"status" json:"status"`
}

type GroupDoc struct {
	bun.BaseModel `bun:"table:group_docs"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string `bun:"org_id" json:"org_id"`
	OwnerID       string `bun:"owner_id" json:"owner_id"`
	ReviewerID    string `bun:"reviewer_id" json:"reviewer_id"`
	Title         string `bun:"title" json:"title"`
}

type groupPostList struct {
	Data   []GroupPost               `json:"data"`
	Counts map[string]map[string]int `json:"counts"`
}

type groupBlogList struct {
	Data   []GroupBlog               `json:"data"`
	Counts map[string]map[string]int `json:"counts"`
}

// seedGroupBlogs creates Alice's blog (with her post) and Bob's blog (with Bob's post that
// Alice edits), and returns the blog and post IDs.
func seedGroupBlogs(t *testing.T) (aliceBlog, bobBlog, alicePost, bobPost int) {
	t.Helper()
	db := hardTables(t, (*GroupBlog)(nil), (*GroupPost)(nil))
	ctx := context.Background()

	blogs := []GroupBlog{{AuthorID: "alice", Name: "Alice's blog"}, {AuthorID: "bob", Name: "Bob's blog"}}
	if _, err := db.NewInsert().Model(&blogs).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	posts := []GroupPost{
		{BlogID: blogs[0].ID, AuthorID: "alice", Title: "alice draft", Status: "draft"},
		{BlogID: blogs[1].ID, AuthorID: "bob", EditorID: "alice", Title: "bob published", Status: "published"},
	}
	if _, err := db.NewInsert().Model(&posts).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return blogs[0].ID, blogs[1].ID, posts[0].ID, posts[1].ID
}

func registerGroupBlogs(b *router.Builder) {
	router.RegisterRoutes[GroupBlog](b, "/blogs",
		router.AllWithOwnershipUnless([]string{"AuthorID"}, "admin"),
		router.WithFilters("Name"),
		func(b *router.Builder) {
			router.RegisterRoutes[GroupPost](b, "/posts",
				router.AllWithOwnershipUnless([]string{"AuthorID", "EditorID"}, "admin"),
				router.AuthConfig{
					Methods:   []string{router.MethodBatchUpdate},
					Ownership: &router.OwnershipConfig{Fields: []string{"AuthorID", "EditorID"}, BypassScopes: []string{"admin"}},
				},
				router.WithRelationName("Posts"),
				router.WithFilters("Status"),
			)
		},
	)
}

func TestOwnershipGrouping_ListStaysUnderURLParent(t *testing.T) {
	aliceBlog, _, alicePost, _ := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"user"})
	registerGroupBlogs(router.NewBuilder(r))

	w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list groupPostList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != alicePost {
		t.Errorf("expected only Alice's post under her blog, got %+v", list.Data)
	}
}

func TestOwnershipGrouping_ClientFiltersApplyToEveryOwnershipField(t *testing.T) {
	aliceBlog, _, _, _ := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"user"})
	registerGroupBlogs(router.NewBuilder(r))

	w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts?filter[Status]=published", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list groupPostList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Errorf("expected no published posts under Alice's blog, got %+v", list.Data)
	}
}

func TestOwnershipGrouping_GetHonoursURLParentAndID(t *testing.T) {
	aliceBlog, bobBlog, _, bobPost := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"user"})
	registerGroupBlogs(router.NewBuilder(r))

	if w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts/"+strconv.Itoa(bobPost), ""); w.Code != http.StatusNotFound {
		t.Errorf("post from another blog under Alice's blog: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts/99999", ""); w.Code != http.StatusNotFound {
		t.Errorf("missing post ID: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(bobBlog)+"/posts/"+strconv.Itoa(bobPost), ""); w.Code != http.StatusNotFound {
		t.Errorf("post under a blog Alice does not own: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOwnershipGrouping_BatchPreFetchHonoursURLParent(t *testing.T) {
	aliceBlog, _, _, bobPost := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"user"})
	registerGroupBlogs(router.NewBuilder(r))

	body := `[{"id":` + strconv.Itoa(bobPost) + `,"title":"hijacked","author_id":"bob","editor_id":"alice","status":"published"}]`
	w := hardRequest(t, r, "PUT", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts/batch", body)
	if w.Code != http.StatusNotFound {
		t.Errorf("batch update of a post from another blog: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOwnershipGrouping_EditorReachesPostThroughRootRoute(t *testing.T) {
	_, bobBlog, alicePost, bobPost := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"user"})
	b := router.NewBuilder(r)
	registerGroupBlogs(b)
	router.RegisterRoutes[GroupPost](b, "/posts",
		router.AuthConfig{
			Methods:   []string{router.MethodGet, router.MethodList},
			Ownership: &router.OwnershipConfig{Fields: []string{"AuthorID", "EditorID"}, BypassScopes: []string{"admin"}},
		},
	)

	if w := hardRequest(t, r, "GET", "/blogs/"+strconv.Itoa(bobBlog)+"/posts/"+strconv.Itoa(bobPost), ""); w.Code != http.StatusNotFound {
		t.Errorf("nested under a blog Alice does not own: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, r, "GET", "/posts/"+strconv.Itoa(bobPost), ""); w.Code != http.StatusOK {
		t.Errorf("root route as editor: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := hardRequest(t, r, "GET", "/posts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list groupPostList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	ids := map[int]bool{}
	for _, p := range list.Data {
		ids[p.ID] = true
	}
	if len(ids) != 2 || !ids[alicePost] || !ids[bobPost] {
		t.Errorf("root list: expected Alice's authored and edited posts, got %+v", list.Data)
	}
}

func TestParentOwnership_CreateUnderAnotherUsersParent(t *testing.T) {
	aliceBlog, _, _, _ := seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "bob", []string{"user"})
	router.RegisterRoutes[GroupBlog](router.NewBuilder(r), "/blogs",
		router.AllWithOwnershipUnless([]string{"AuthorID"}, "admin"),
		func(b *router.Builder) {
			router.RegisterRoutes[GroupPost](b, "/posts", router.IsAuthenticated())
		},
	)

	w := hardRequest(t, r, "POST", "/blogs/"+strconv.Itoa(aliceBlog)+"/posts", `{"title":"not bob's blog"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("create under a blog Bob does not own: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOwnershipGrouping_TenantAppliesToEveryOwnershipField(t *testing.T) {
	db := hardTables(t, (*GroupDoc)(nil))
	docs := []GroupDoc{
		{OrgID: "org-a", OwnerID: "alice", Title: "own tenant"},
		{OrgID: "org-b", OwnerID: "alice", Title: "other tenant"},
	}
	if _, err := db.NewInsert().Model(&docs).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	r := addTenantAuthMiddleware(chi.NewRouter(), "alice", "org-a", []string{"user"})
	router.RegisterRoutes[GroupDoc](router.NewBuilder(r), "/docs",
		router.WithTenantScope("OrgID"),
		router.AllWithOwnershipUnless([]string{"OwnerID", "ReviewerID"}, "admin"),
	)

	w := hardRequest(t, r, "GET", "/docs", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "other tenant") {
		t.Errorf("list leaked a row from another tenant: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "own tenant") {
		t.Errorf("list is missing the caller's own row: %s", w.Body.String())
	}
}

// Issue #144: filters on parent fields must be authorized like ?include= of the parent.

func TestParentFieldFilter_RequiresParentAuth(t *testing.T) {
	aliceBlog, _, _, _ := seedGroupBlogs(t)

	register := func(b *router.Builder) {
		router.RegisterRoutes[GroupBlog](b, "/blogs",
			router.AllScoped("blog:read"),
			router.WithFilters("Name"),
			func(b *router.Builder) {
				router.RegisterRoutes[GroupPost](b, "/posts",
					router.AllScoped("post:read"),
					router.WithRelationName("Posts"),
				)
			},
		)
	}
	path := "/blogs/" + strconv.Itoa(aliceBlog) + "/posts?filter[Blog.Name]=no-such-blog"

	withoutParent := addAuthMiddleware(chi.NewRouter(), "alice", []string{"post:read"})
	register(router.NewBuilder(withoutParent))
	w := hardRequest(t, withoutParent, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list groupPostList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 {
		t.Errorf("parent filter without parent auth must be ignored: expected 1 post, got %d", len(list.Data))
	}

	withParent := addAuthMiddleware(chi.NewRouter(), "alice", []string{"post:read", "blog:read"})
	register(router.NewBuilder(withParent))
	w = hardRequest(t, withParent, "GET", path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	list = groupPostList{}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Errorf("parent filter with parent auth must apply: expected 0 posts, got %d", len(list.Data))
	}
}

// Relation filters and counts must not treat a caller authorized for no relations as
// unrestricted.

func TestRelationFilters_IgnoredWhenNoRelationIsAuthorized(t *testing.T) {
	seedGroupBlogs(t)

	r := addAuthMiddleware(chi.NewRouter(), "alice", []string{"blog:read"})
	router.RegisterRoutes[GroupBlog](router.NewBuilder(r), "/blogs",
		router.AllScoped("blog:read"),
		func(b *router.Builder) {
			router.RegisterRoutes[GroupPost](b, "/posts",
				router.AllScoped("post:read"),
				router.WithRelationName("Posts"),
			)
		},
	)

	w := hardRequest(t, r, "GET", "/blogs?filter[Posts][exists]=false&include_count=Posts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list groupBlogList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 {
		t.Errorf("unauthorized relation filter must be ignored: expected both blogs, got %d", len(list.Data))
	}
	if list.Counts != nil {
		t.Errorf("unauthorized include_count must be omitted, got %v", list.Counts)
	}
}
