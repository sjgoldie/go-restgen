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

// CUAccount is keyed by its own ID; Subject holds the auth user ID.
type CUAccount struct {
	bun.BaseModel `bun:"table:cu_accounts"`
	ID            int       `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string    `bun:"org_id" json:"org_id"`
	Subject       string    `bun:"subject" json:"subject"`
	Name          string    `bun:"name" json:"name"`
	Notes         []*CUNote `bun:"rel:has-many,join:id=account_id" json:"notes,omitempty"`
}

type CUNote struct {
	bun.BaseModel `bun:"table:cu_notes"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	OrgID         string `bun:"org_id" json:"org_id"`
	AccountID     int    `bun:"account_id" json:"account_id"`
	Text          string `bun:"text" json:"text"`
}

type cuAccountFixture struct {
	alice, bob, aliceOrgB int
	bobNote               int
}

// seedAccounts creates accounts alice and bob in org-a, a second alice in org-b, and a note each
// for alice (org-a) and bob.
func seedAccounts(t *testing.T) cuAccountFixture {
	t.Helper()
	db := hardTables(t, (*CUAccount)(nil), (*CUNote)(nil))
	ctx := context.Background()

	accounts := []CUAccount{
		{OrgID: "org-a", Subject: "alice", Name: "Alice"},
		{OrgID: "org-a", Subject: "bob", Name: "Bob"},
		{OrgID: "org-b", Subject: "alice", Name: "Alice B"},
	}
	if _, err := db.NewInsert().Model(&accounts).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	notes := []CUNote{{OrgID: "org-a", AccountID: accounts[0].ID, Text: "alice-note"}, {OrgID: "org-a", AccountID: accounts[1].ID, Text: "bob-note"}}
	if _, err := db.NewInsert().Model(&notes).Returning("*").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return cuAccountFixture{alice: accounts[0].ID, bob: accounts[1].ID, aliceOrgB: accounts[2].ID, bobNote: notes[1].ID}
}

// registerAccountMe serves the caller's account at /me, found by Subject, scoped to the caller's
// org, with their notes nested under it.
func registerAccountMe(b *router.Builder) {
	router.RegisterRoutes[CUAccount](b, "/me",
		router.AsCurrentUserExternal("Subject"),
		router.WithTenantScope("OrgID"),
		router.AuthConfig{Methods: []string{router.MethodGet, router.MethodPatch}, Scopes: []string{router.ScopeAuthOnly}},
		func(b *router.Builder) {
			router.RegisterRoutes[CUNote](b, "/notes", router.IsAuthenticated(), router.WithRelationName("Notes"))
		},
	)
}

func orgCaller(userID, org string) *router.AuthInfo {
	return &router.AuthInfo{UserID: userID, TenantID: org}
}

func decodeAccount(t *testing.T, body []byte) CUAccount {
	t.Helper()
	var a CUAccount
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("decode account: %v: %s", err, body)
	}
	return a
}

func storedAccount(t *testing.T, id int) CUAccount {
	t.Helper()
	var a CUAccount
	if err := hardTables(t).NewSelect().Model(&a).Where("id = ?", id).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCurrentUserExternal_Get(t *testing.T) {
	f := seedAccounts(t)

	w := hardRequest(t, scopedRouter(orgCaller("alice", "org-a"), registerAccountMe), "GET", "/me", "")
	if w.Code != http.StatusOK || decodeAccount(t, w.Body.Bytes()).ID != f.alice {
		t.Errorf("alice in org-a: expected her account, got %d: %s", w.Code, w.Body.String())
	}
	w = hardRequest(t, scopedRouter(orgCaller("alice", "org-b"), registerAccountMe), "GET", "/me", "")
	if w.Code != http.StatusOK || decodeAccount(t, w.Body.Bytes()).ID != f.aliceOrgB {
		t.Errorf("alice in org-b: expected the org-b account, got %d: %s", w.Code, w.Body.String())
	}

	if w := hardRequest(t, scopedRouter(orgCaller("carol", "org-a"), registerAccountMe), "GET", "/me", ""); w.Code != http.StatusNotFound {
		t.Errorf("no matching account: expected 404, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(orgCaller("bob", "org-b"), registerAccountMe), "GET", "/me", ""); w.Code != http.StatusNotFound {
		t.Errorf("account in another org: expected 404, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(nil, registerAccountMe), "GET", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(&router.AuthInfo{UserID: "alice"}, registerAccountMe), "GET", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no tenant on a tenant route: expected 401, got %d", w.Code)
	}
	if w := hardRequest(t, scopedRouter(orgCaller("alice", "org-a"), registerAccountMe), "GET", "/me/"+strconv.Itoa(f.bob), ""); w.Code != http.StatusNotFound {
		t.Errorf("/me/{id}: expected 404, got %d", w.Code)
	}
}

func TestCurrentUserExternal_Writes(t *testing.T) {
	f := seedAccounts(t)
	alice := scopedRouter(orgCaller("alice", "org-a"), registerAccountMe)

	body := `{"id":` + strconv.Itoa(f.bob) + `,"subject":"bob","org_id":"org-b","name":"Alice Two"}`
	w := hardRequest(t, alice, "PATCH", "/me", body)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := storedAccount(t, f.alice)
	if got.Name != "Alice Two" || got.Subject != "alice" || got.OrgID != "org-a" {
		t.Errorf("only the name may change: got %+v", got)
	}
	if bob := storedAccount(t, f.bob); bob.Name != "Bob" || bob.Subject != "bob" {
		t.Errorf("bob's account must be untouched: got %+v", bob)
	}

	if w := hardRequest(t, alice, "DELETE", "/me", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("method without an auth config: expected 401, got %d", w.Code)
	}
}

func TestCurrentUserExternal_Children(t *testing.T) {
	f := seedAccounts(t)
	alice := scopedRouter(orgCaller("alice", "org-a"), registerAccountMe)

	var list struct {
		Data []CUNote `json:"data"`
	}
	w := hardRequest(t, alice, "GET", "/me/notes", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Data) != 1 || list.Data[0].Text != "alice-note" {
		t.Errorf("expected only alice's note, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, alice, "GET", "/me/notes/"+strconv.Itoa(f.bobNote), ""); w.Code != http.StatusNotFound {
		t.Errorf("another account's note: expected 404, got %d", w.Code)
	}

	w = hardRequest(t, alice, "POST", "/me/notes", `{"account_id":`+strconv.Itoa(f.bob)+`,"text":"new"}`)
	var created CUNote
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != http.StatusCreated {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}
	if created.AccountID != f.alice {
		t.Errorf("a created note belongs to the caller's account %d, got %d", f.alice, created.AccountID)
	}

	if w := hardRequest(t, scopedRouter(orgCaller("carol", "org-a"), registerAccountMe), "GET", "/me/notes", ""); w.Code != http.StatusNotFound {
		t.Errorf("children of a caller without an account: expected 404, got %d", w.Code)
	}
}

func TestCurrentUserExternal_AmbiguousFieldMatchesNothing(t *testing.T) {
	seedAccounts(t)
	r := scopedRouter(&router.AuthInfo{UserID: "alice"}, func(b *router.Builder) {
		router.RegisterRoutes[CUAccount](b, "/me", router.AsCurrentUserExternal("Subject"), router.IsAuthenticated())
	})
	if w := hardRequest(t, r, "GET", "/me", ""); w.Code != http.StatusNotFound {
		t.Errorf("two accounts with the caller's subject: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCurrentUserExternal_UnusableFieldIsNotRegistered(t *testing.T) {
	seedAccounts(t)
	for name, field := range map[string]string{"unknown field": "Missing", "primary key": "ID"} {
		t.Run(name, func(t *testing.T) {
			r := scopedRouter(orgCaller("alice", "org-a"), func(b *router.Builder) {
				router.RegisterRoutes[CUAccount](b, "/me", router.AsCurrentUserExternal(field), router.IsAuthenticated())
			})
			if w := hardRequest(t, r, "GET", "/me", ""); w.Code != http.StatusNotFound {
				t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// CUNoTable has no table, so looking up the caller fails.
type CUNoTable struct {
	bun.BaseModel `bun:"table:cu_no_table"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	Subject       string `bun:"subject,unique" json:"subject"`
}

type CUPairKey struct {
	bun.BaseModel `bun:"table:cu_pair_keys"`
	A             int    `bun:"a,pk" json:"a"`
	B             int    `bun:"b,pk" json:"b"`
	Subject       string `bun:"subject" json:"subject"`
}

func TestCurrentUserExternal_LookupErrorIs500(t *testing.T) {
	r := scopedRouter(&router.AuthInfo{UserID: "alice"}, func(b *router.Builder) {
		router.RegisterRoutes[CUNoTable](b, "/me", router.AsCurrentUserExternal("Subject"), router.IsAuthenticated())
	})
	if w := hardRequest(t, r, "GET", "/me", ""); w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCurrentUserExternal_CompositeKeyIsNotRegistered(t *testing.T) {
	r := scopedRouter(&router.AuthInfo{UserID: "alice"}, func(b *router.Builder) {
		router.RegisterRoutes[CUPairKey](b, "/me", router.AsCurrentUserExternal("Subject"), router.IsAuthenticated())
	})
	if w := hardRequest(t, r, "GET", "/me", ""); w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
