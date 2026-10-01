package router_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/uptrace/bun"

	"github.com/sjgoldie/go-restgen/metadata"
	"github.com/sjgoldie/go-restgen/router"
	"github.com/sjgoldie/go-restgen/service"
)

type RSInvoice struct {
	bun.BaseModel `bun:"table:rs_invoices"`
	ID            int    `bun:"id,pk,autoincrement" json:"id"`
	OwnerID       string `bun:"owner_id" json:"owner_id"`
	Number        string `bun:"number" json:"number"`
}

// textReport is an app-defined Responder writing a plain text body.
type textReport struct{ body string }

func (r textReport) WriteResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(r.body))
	return err
}

func invoiceReport(_ context.Context, _ *service.Common[RSInvoice], _ *metadata.TypeMetadata, _ *metadata.AuthInfo, _ string, invoice *RSInvoice, _ []byte) (any, int, error) {
	return textReport{body: "Invoice " + invoice.Number}, 0, nil
}

func TestEndpoint_ResponderThroughTheRouter(t *testing.T) {
	db := hardTables(t, (*RSInvoice)(nil))
	invoices := []RSInvoice{{OwnerID: "alice", Number: "A-1"}, {OwnerID: "bob", Number: "B-1"}}
	if _, err := db.NewInsert().Model(&invoices).Returning("*").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	register := func(b *router.Builder) {
		router.RegisterRoutes[RSInvoice](b, "/invoices",
			router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin"),
			router.WithEndpoint("GET", "report", invoiceReport, router.AllWithOwnershipUnless([]string{"OwnerID"}, "admin")),
		)
	}
	alice := scopedRouter(asCaller("alice"), register)
	report := func(id int) string { return "/invoices/" + strconv.Itoa(id) + "/report" }

	w := hardRequest(t, alice, "GET", report(invoices[0].ID), "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Body.String() != "Invoice A-1" {
		t.Errorf("own invoice: got %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if w := hardRequest(t, alice, "GET", report(invoices[1].ID), ""); w.Code != http.StatusNotFound {
		t.Errorf("another user's invoice: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := hardRequest(t, scopedRouter(nil, register), "GET", report(invoices[0].ID), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", w.Code)
	}
}
