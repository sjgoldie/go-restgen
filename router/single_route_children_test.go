package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/router"
)

type SRPost struct {
	bun.BaseModel `bun:"table:sr_posts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	OwnerID       string    `bun:"owner_id" json:"owner_id"`
	AuthorID      int       `bun:"author_id,nullzero" json:"author_id,omitempty"`
	Author        *SRAuthor `bun:"rel:belongs-to,join:author_id=id" json:"author,omitempty"`
}

type SRAuthor struct {
	bun.BaseModel `bun:"table:sr_authors"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	Name          string    `bun:"name" json:"name"`
	Books         []*SRBook `bun:"rel:has-many,join:id=author_id" json:"books,omitempty"`
}

type SRBook struct {
	bun.BaseModel `bun:"table:sr_books"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	AuthorID      int    `bun:"author_id" json:"author_id"`
	Title         string `bun:"title" json:"title"`
}

type srFixture struct {
	alicePost, bobPost, unauthoredPost int
	one, two                           int
	byOne, byTwo                       int
}

// seedSingleRouteChildren creates authors one and two, posts by alice (author two), bob (author
// one) and alice (no author), and a book by each author. Post and author IDs differ, so a post's
// ID is never its author's ID.
func seedSingleRouteChildren(t *testing.T) srFixture {
	t.Helper()
	db := hardTables(t, (*SRPost)(nil), (*SRAuthor)(nil), (*SRBook)(nil))
	ctx := context.Background()

	authors := []SRAuthor{{Name: "one"}, {Name: "two"}}
	if _, err := db.NewInsert().Model(&authors).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	posts := []SRPost{
		{OwnerID: "alice", AuthorID: authors[1].ID},
		{OwnerID: "bob", AuthorID: authors[0].ID},
		{OwnerID: "alice"},
	}
	if _, err := db.NewInsert().Model(&posts).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	books := []SRBook{{AuthorID: authors[0].ID, Title: "by-one"}, {AuthorID: authors[1].ID, Title: "by-two"}}
	if _, err := db.NewInsert().Model(&books).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return srFixture{
		alicePost: posts[0].ID, bobPost: posts[1].ID, unauthoredPost: posts[2].ID,
		one: authors[0].ID, two: authors[1].ID,
		byOne: books[0].ID, byTwo: books[1].ID,
	}
}

// registerPostAuthorBooks registers owned posts, each post's author as a single route, and the
// author's books nested under it.
func registerPostAuthorBooks(b *router.Builder) {
	router.RegisterRoutes[SRPost](b, "/posts",
		router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
		func(b *router.Builder) {
			router.RegisterRoutes[SRAuthor](b, "/author",
				router.AsSingleRouteWithUpdate("AuthorID"),
				router.IsAuthenticated(),
				router.WithRelationName("Author"),
				func(b *router.Builder) {
					router.RegisterRoutes[SRBook](b, "/books", router.IsAuthenticated(), router.WithRelationName("Books"))
				},
			)
		},
	)
}

func listBooks(t *testing.T, r http.Handler, path string) (int, []SRBook) {
	t.Helper()
	w := hardRequest(t, r, "GET", path, "")
	var list struct {
		Data []SRBook `json:"data"`
	}
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, list.Data
}

func postPath(post int, rest string) string {
	return "/posts/" + strconv.Itoa(post) + rest
}

func TestSingleRouteChildren_ScopedToTheParentsRow(t *testing.T) {
	f := seedSingleRouteChildren(t)
	alice := scopedRouter(asCaller("alice"), registerPostAuthorBooks)
	admin := scopedRouter(&router.AuthInfo{UserID: "root", Scopes: []string{"admin"}}, registerPostAuthorBooks)

	code, books := listBooks(t, alice, postPath(f.alicePost, "/author/books"))
	if code != http.StatusOK || len(books) != 1 || books[0].Title != "by-two" {
		t.Errorf("alice's post: expected the books of its author (two), got %d %+v", code, books)
	}
	code, books = listBooks(t, admin, postPath(f.bobPost, "/author/books"))
	if code != http.StatusOK || len(books) != 1 || books[0].Title != "by-one" {
		t.Errorf("bob's post: expected the books of its author (one), got %d %+v", code, books)
	}

	if w := hardRequest(t, alice, "GET", postPath(f.alicePost, "/author/books/"+strconv.Itoa(f.byTwo)), ""); w.Code != http.StatusOK {
		t.Errorf("a book by the post's author: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, alice, "GET", postPath(f.alicePost, "/author/books/"+strconv.Itoa(f.byOne)), ""); w.Code != http.StatusNotFound {
		t.Errorf("a book by another author: expected 404, got %d", w.Code)
	}
	if w := hardRequest(t, alice, "PATCH", postPath(f.alicePost, "/author/books/"+strconv.Itoa(f.byOne)), `{"title":"taken"}`); w.Code != http.StatusNotFound {
		t.Errorf("updating a book by another author: expected 404, got %d", w.Code)
	}
}

func TestSingleRouteChildren_Create(t *testing.T) {
	f := seedSingleRouteChildren(t)
	alice := scopedRouter(asCaller("alice"), registerPostAuthorBooks)

	w := hardRequest(t, alice, "POST", postPath(f.alicePost, "/author/books"), `{"author_id":`+strconv.Itoa(f.one)+`,"title":"new"}`)
	var created SRBook
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}
	if created.AuthorID != f.two {
		t.Errorf("a book created under the post's author belongs to author %d, got %d", f.two, created.AuthorID)
	}
}

func TestSingleRouteChildren_ParentAccess(t *testing.T) {
	f := seedSingleRouteChildren(t)
	alice := scopedRouter(asCaller("alice"), registerPostAuthorBooks)

	code, books := listBooks(t, alice, postPath(f.bobPost, "/author/books"))
	if len(books) != 0 || (code != http.StatusOK && code != http.StatusNotFound) {
		t.Errorf("a post alice does not own: expected no books, got %d %+v", code, books)
	}
	if w := hardRequest(t, alice, "POST", postPath(f.bobPost, "/author/books"), `{"title":"sneaky"}`); w.Code == http.StatusCreated {
		t.Errorf("creating under a post alice does not own must fail, got 201")
	}
	if code, _ := listBooks(t, alice, postPath(f.unauthoredPost, "/author/books")); code != http.StatusNotFound {
		t.Errorf("a post with no author: expected 404, got %d", code)
	}
	if code, _ := listBooks(t, alice, postPath(9999, "/author/books")); code != http.StatusNotFound {
		t.Errorf("a missing post: expected 404, got %d", code)
	}
}

func TestSingleRouteChildren_SingleRouteUnchanged(t *testing.T) {
	f := seedSingleRouteChildren(t)
	alice := scopedRouter(asCaller("alice"), registerPostAuthorBooks)

	w := hardRequest(t, alice, "GET", postPath(f.alicePost, "/author?include=Books"), "")
	var author SRAuthor
	if err := json.Unmarshal(w.Body.Bytes(), &author); err != nil || w.Code != http.StatusOK {
		t.Fatalf("get: %d: %s", w.Code, w.Body.String())
	}
	if author.ID != f.two || len(author.Books) != 1 || author.Books[0].Title != "by-two" {
		t.Errorf("expected author two with their book, got %+v", author)
	}

	w = hardRequest(t, alice, "PUT", postPath(f.alicePost, "/author"), `{"id":`+strconv.Itoa(f.one)+`,"name":"two renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d: %s", w.Code, w.Body.String())
	}
	var renamed, untouched SRAuthor
	db := hardTables(t)
	_ = db.NewSelect().Model(&renamed).Where("id = ?", f.two).Scan(context.Background())
	_ = db.NewSelect().Model(&untouched).Where("id = ?", f.one).Scan(context.Background())
	if renamed.Name != "two renamed" || untouched.Name != "one" {
		t.Errorf("put must update the post's author only: two %q, one %q", renamed.Name, untouched.Name)
	}

	if w := hardRequest(t, alice, "GET", postPath(f.unauthoredPost, "/author"), ""); w.Code != http.StatusNotFound {
		t.Errorf("a post with no author: expected 404, got %d", w.Code)
	}
}
