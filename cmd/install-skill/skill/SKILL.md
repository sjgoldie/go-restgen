---
name: go-restgen
description: go-restgen REST API framework patterns — model definitions, route registration, auth, nesting, custom endpoints, SSE, file uploads, tenancy, and query configuration.
---

# go-restgen Framework Guide

You are working with go-restgen, a type-safe REST API framework for Go using generics. It auto-generates CRUD endpoints from model definitions. Built on Chi (router) and Bun (ORM).

Read `patterns.md` in this skill directory for complete handler signatures, all route options, and detailed examples.

## Core Pattern

```go
// 1. Define model with Bun tags
type Product struct {
    bun.BaseModel `bun:"table:products"`
    ID            int       `bun:"id,pk,autoincrement" json:"id"`
    Name          string    `bun:"name,notnull" json:"name"`
    Price         float64   `bun:"price,notnull" json:"price"`
    CreatedAt     time.Time `bun:"created_at,notnull,skipupdate" json:"created_at,omitempty"`
    UpdatedAt     time.Time `bun:"updated_at,notnull" json:"updated_at,omitempty"`
}

// 2. Timestamp hook
func (p *Product) BeforeAppendModel(ctx context.Context, query bun.Query) error {
    now := time.Now()
    switch query.(type) {
    case *bun.InsertQuery:
        p.CreatedAt = now
        p.UpdatedAt = now
    case *bun.UpdateQuery:
        p.UpdatedAt = now
    }
    return nil
}

// 3. Setup: datastore, chi router, builder
db, _ := datastore.NewSQLite(":memory:")
datastore.Initialize(db)
defer datastore.Cleanup()
db.GetDB().NewCreateTable().Model((*Product)(nil)).IfNotExists().Exec(context.Background())

r := chi.NewRouter()
b := router.NewBuilder(r)

// 4. Register routes with options
router.RegisterRoutes[Product](b, "/products",
    router.AllPublic(),
    router.WithFilters("Name", "Price"),
    router.WithSorts("Name", "Price", "CreatedAt"),
    router.WithPagination(20, 100),
)
```

This generates: `GET/POST /products`, `GET/PUT/PATCH/DELETE /products/{id}`.

## Auth Patterns

```go
router.AllPublic()                                          // No auth
router.AllScoped("admin")                                   // Require scope
router.IsAuthenticated()                                    // Any valid auth
router.AllWithOwnershipUnless([]string{"UserID"}, "admin")  // Owner-only + admin bypass

// Per-method
router.AuthConfig{
    Methods: []string{router.MethodGet, router.MethodList},
    Scopes:  []string{router.ScopePublic},
},
router.AuthConfig{
    Methods: []string{router.MethodPost, router.MethodPut, router.MethodPatch, router.MethodDelete},
    Scopes:  []string{"admin"},
},
```

Auth middleware sets `AuthInfo` on context:
```go
authInfo := &router.AuthInfo{UserID: userID, TenantID: tenantID, Scopes: scopes}
ctx := context.WithValue(r.Context(), router.AuthInfoKey, authInfo)
```

## Nesting

```go
router.RegisterRoutes[Blog](b, "/blogs", router.AllPublic(), func(b *router.Builder) {
    router.RegisterRoutes[Post](b, "/posts",
        router.AllPublic(),
        router.WithRelationName("Posts"),  // enables ?include=Posts on parent
    )
})
```

Child model needs FK + belongs-to tag:
```go
BlogID int `bun:"blog_id,notnull,skipupdate" json:"blog_id"`
Blog  *Blog `bun:"rel:belongs-to,join:blog_id=id" json:"-"`
```

## Custom Endpoints (Anything Funcs)

```go
// Item-level: METHOD /resource/{id}/{name}
router.WithEndpoint("GET", "status", handlerFn, router.AuthConfig{...})

// Root-level: METHOD /any/path (no parent model)
router.RegisterRootEndpoint(b, "GET", "/system/info", handlerFn, router.AllPublic())

// Item-level SSE: GET /resource/{id}/{name}
router.WithSSE("events", sseFn, router.AuthConfig{...})

// Root-level SSE: GET /any/path
router.RegisterRootSSE(b, "/events/system", sseFn, router.AllPublic())
```

## Key Conventions

- **PK field**: named `ID` by default, override with `router.WithAlternatePK("MyPK")`
- **FK fields**: include `skipupdate` to prevent modification
- **Ownership fields**: string type matching `AuthInfo.UserID`
- **Timestamps**: use `BeforeAppendModel` hook
- **UUID PKs**: use `uuid.UUID` type with `bun:"id,pk,type:uuid"`
- **Routes blocked by default**: must explicitly configure auth
- **Path IDs take precedence**: JSON body IDs and FKs are ignored
