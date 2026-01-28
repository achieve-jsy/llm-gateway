package model

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm/schema"
)

func init() {
	schema.RegisterSerializer("tztime", tzTimeSerializer{})
}

var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999-0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05-0700",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseTimeString(s string) (time.Time, error) {
	for _, layout := range timeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("can't parse time: %s", s)
}

type tzTimeSerializer struct{}

func (tzTimeSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	fieldVal := field.ReflectValueOf(ctx, dst)
	if dbValue == nil {
		fieldVal.Set(reflect.Zero(field.FieldType))
		return nil
	}
	var t time.Time
	switch v := dbValue.(type) {
	case time.Time:
		t = v
	case string:
		if v == "" {
			fieldVal.Set(reflect.Zero(field.FieldType))
			return nil
		}
		parsed, err := parseTimeString(v)
		if err != nil {
			return fmt.Errorf("failed to parse time %q for field %s: %w", v, field.Name, err)
		}
		t = parsed
	case []byte:
		if len(v) == 0 {
			fieldVal.Set(reflect.Zero(field.FieldType))
			return nil
		}
		parsed, err := parseTimeString(string(v))
		if err != nil {
			return fmt.Errorf("failed to parse time %q for field %s: %w", string(v), field.Name, err)
		}
		t = parsed
	default:
		return fmt.Errorf("unsupported type %T for time field %s", dbValue, field.Name)
	}
	if field.FieldType == reflect.TypeOf(time.Time{}) {
		fieldVal.Set(reflect.ValueOf(t))
	} else {
		fieldVal.Set(reflect.ValueOf(&t))
	}
	return nil
}

func (tzTimeSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue interface{}) (interface{}, error) {
	switch v := fieldValue.(type) {
	case time.Time:
		return v, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	default:
		return fieldValue, nil
	}
}
