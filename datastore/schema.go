package datastore

import (
	"fmt"
	"reflect"

	"github.com/uptrace/bun/schema"
)

// Relation holds the result of finding a parent-child relationship via Bun schema.
type Relation struct {
	ForeignKeyCol string // FK column name (e.g., "author_id")
	ParentJoinCol string // Parent join column name (e.g., "id")
	FieldName     string // Struct field name for the relation (e.g., "Author")
}

// ColumnName resolves a Go struct field name to its SQL column name using Bun's schema.
func ColumnName(tType reflect.Type, goName string) (string, error) {
	store, err := Get()
	if err != nil {
		return "", err
	}
	table := store.GetDB().Table(tType)
	for _, field := range table.Fields {
		if field.GoName == goName {
			return field.Name, nil
		}
	}
	return "", fmt.Errorf("field %s not found on type %s", goName, tType.Name())
}

// FieldName resolves a SQL column name back to its Go struct field name using Bun's schema.
func FieldName(tType reflect.Type, colName string) (string, error) {
	store, err := Get()
	if err != nil {
		return "", err
	}
	table := store.GetDB().Table(tType)
	for _, field := range table.Fields {
		if field.Name == colName {
			return field.GoName, nil
		}
	}
	return "", fmt.Errorf("column %s not found on type %s", colName, tType.Name())
}

// TableName returns the SQL table name for a model type using Bun's schema.
func TableName(tType reflect.Type) string {
	store, err := Get()
	if err != nil {
		return ""
	}
	table := store.GetDB().Table(tType)
	return table.Name
}

// FindRelation uses Bun's schema to find the relationship between child and parent types.
// Checks for:
// 1. Child has belongs-to Parent (e.g., Article belongs-to User) — FK is on child
// 2. Parent has has-many/has-one Child — extract join columns from parent's relation
// 3. Parent has belongs-to Child (inverted) — FK is on parent
func FindRelation(childType, parentType reflect.Type) (Relation, error) {
	if parentType == nil {
		return Relation{}, nil
	}

	store, err := Get()
	if err != nil {
		return Relation{}, err
	}
	db := store.GetDB()

	childTable := db.Table(childType)

	// Case 1: child has a belongs-to relation pointing to parent
	for _, rel := range childTable.Relations {
		if rel.Type == schema.BelongsToRelation && rel.JoinTable.Type == parentType {
			if len(rel.BasePKs) > 0 && len(rel.JoinPKs) > 0 {
				return Relation{
					ForeignKeyCol: rel.BasePKs[0].Name,
					ParentJoinCol: rel.JoinPKs[0].Name,
					FieldName:     rel.Field.GoName,
				}, nil
			}
		}
	}

	// Case 2: parent has has-many or has-one pointing to child
	parentTable := db.Table(parentType)
	for _, rel := range parentTable.Relations {
		if (rel.Type == schema.HasManyRelation || rel.Type == schema.HasOneRelation) && rel.JoinTable.Type == childType {
			if len(rel.BasePKs) > 0 && len(rel.JoinPKs) > 0 {
				return Relation{
					ForeignKeyCol: rel.JoinPKs[0].Name,
					ParentJoinCol: rel.BasePKs[0].Name,
					FieldName:     rel.Field.GoName,
				}, nil
			}
		}
	}

	// Case 3: parent has belongs-to pointing to child (inverted belongs-to,
	// e.g., Post.Author belongs-to User — FK author_id is on the parent)
	for _, rel := range parentTable.Relations {
		if rel.Type == schema.BelongsToRelation && rel.JoinTable.Type == childType {
			if len(rel.BasePKs) > 0 && len(rel.JoinPKs) > 0 {
				return Relation{
					ForeignKeyCol: rel.BasePKs[0].Name,
					ParentJoinCol: rel.JoinPKs[0].Name,
					FieldName:     rel.Field.GoName,
				}, nil
			}
		}
	}

	return Relation{}, fmt.Errorf("no relationship between %s and %s found", childType.Name(), parentType.Name())
}
