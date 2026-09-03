package service_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	apperrors "github.com/sjgoldie/go-restgen/errors"
	"github.com/sjgoldie/go-restgen/service"
)

func TestGetMany_ReturnsRowsInRequestedOrder(t *testing.T) {
	ctx := context.Background()
	_, _ = testDB.GetDB().NewDelete().Model((*TestModel)(nil)).Where("1=1").Exec(ctx)

	first := &TestModel{Name: "first", Email: "getmany-first@example.com"}
	second := &TestModel{Name: "second", Email: "getmany-second@example.com"}
	for _, m := range []*TestModel{first, second} {
		if _, err := testDB.GetDB().NewInsert().Model(m).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}

	svc, err := service.New[TestModel]()
	if err != nil {
		t.Fatal(err)
	}

	items, err := svc.GetMany(ctxWithMeta(testModelMeta), []string{strconv.Itoa(second.ID), strconv.Itoa(first.ID)})
	if err != nil {
		t.Fatal("GetMany failed:", err)
	}
	if len(items) != 2 || items[0].Name != "second" || items[1].Name != "first" {
		t.Errorf("expected [second first], got %+v", items)
	}

	if _, err := svc.GetMany(ctxWithMeta(testModelMeta), []string{strconv.Itoa(first.ID), "999999"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("missing ID: expected ErrNotFound, got %v", err)
	}
}
