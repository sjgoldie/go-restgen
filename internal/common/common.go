package common

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/google/uuid"
)

// SetFieldFromString sets a named struct field from a string value.
// Handles int, int64, string, and uuid.UUID field types.
func SetFieldFromString(item any, fieldName, value string) error {
	field := reflect.ValueOf(item).Elem().FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() {
		return fmt.Errorf("field %q not found or not settable", fieldName)
	}

	switch field.Kind() {
	case reflect.Int, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(intVal)
	case reflect.String:
		field.SetString(value)
	default:
		if field.Type() == reflect.TypeOf(uuid.UUID{}) {
			parsed, err := uuid.Parse(value)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(parsed))
		} else {
			return fmt.Errorf("unsupported field type: %s", field.Type().String())
		}
	}

	return nil
}

// GetFieldAsString reads a named struct field and returns its value as a string.
// Returns an empty string if the field is not found.
func GetFieldAsString(item any, fieldName string) string {
	v := reflect.ValueOf(item)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return ""
	}
	return fmt.Sprintf("%v", field.Interface())
}
