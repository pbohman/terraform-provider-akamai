package tools

import (
	"errors"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var (
	// ErrInvalidType is returned when given value is of invalid type (i.e. different type than requested)
	ErrInvalidType = errors.New("value must be of the specified type")
	// ErrNotFound is returned when value is not present on schema
	ErrNotFound = errors.New("value not found")
	// ErrValueSet is returned when setting property value returned an error
	ErrValueSet = errors.New("setting property value")
	// ErrValueSet is returned when setting property value returned an error
	ErrEmptyKey = errors.New("provided key cannot be empty")
)

type ResourceDataFetcher interface {
	GetOk(string) (interface{}, bool)
}

// GetStringValue fetches value with given key from ResourceData object and attempts type cast to string
//
// if value is not present on provided resource, ErrNotFound is returned
// if casting is not successful, ErrInvalidType is returned
func GetStringValue(key string, rd ResourceDataFetcher) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: %s", ErrEmptyKey, key)
	}
	value, ok := rd.GetOk(key)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	var str string
	if str, ok = value.(string); !ok {
		return "", fmt.Errorf("%w: %s, %q", ErrInvalidType, key, "string")
	}
	return str, nil
}

// GetIntValue fetches value with given key from ResourceData object and attempts type cast to int
//
// if value is not present on provided resource, ErrNotFound is returned
// if casting is not successful, ErrInvalidType is returned
func GetIntValue(key string, rd ResourceDataFetcher) (int, error) {
	if key == "" {
		return 0, fmt.Errorf("%w: %s", ErrEmptyKey, key)
	}
	value, ok := rd.GetOk(key)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	var num int
	if num, ok = value.(int); !ok {
		return 0, fmt.Errorf("%w: %s, %q", ErrInvalidType, key, "int")
	}
	return num, nil
}

// GetBoolValue fetches value with given key from ResourceData object and attempts type cast to bool
//
// if value is not present on provided resource, ErrNotFound is returned
// if casting is not successful, ErrInvalidType is returned
func GetBoolValue(key string, rd ResourceDataFetcher) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("%w: %s", ErrEmptyKey, key)
	}
	value, ok := rd.GetOk(key)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	var val bool
	if val, ok = value.(bool); !ok {
		return false, fmt.Errorf("%w: %s, %q", ErrInvalidType, key, "bool")
	}
	return val, nil
}

// GetSetValue fetches value with given key from ResourceData object and attempts type cast to *schema.Set
//
// if value is not present on provided resource, ErrNotFound is returned
// if casting is not successful, ErrInvalidType is returned
func GetSetValue(key string, rd ResourceDataFetcher) (*schema.Set, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: %s", ErrEmptyKey, key)
	}
	value, ok := rd.GetOk(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	var val *schema.Set
	if val, ok = value.(*schema.Set); !ok {
		return nil, fmt.Errorf("%w: %s, %q", ErrInvalidType, key, "*schema.Set")
	}
	return val, nil
}
