// Package utils provides common validation and utility functions
package utils

import (
	"fmt"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// ObjectType validation

// ValidObjectTypes returns all valid ObjectType values
func ValidObjectTypes() []types.ObjectType {
	return []types.ObjectType{
		types.TypeTable,
		types.TypeIndex,
		types.TypeFunction,
		types.TypeTrigger,
		types.TypeView,
		types.TypeSequence,
		types.TypeConstraint,
		types.TypePolicy,
		types.TypeRole,
		types.TypeSchema,
		types.TypeExtension,
		types.TypePublication,
		types.TypeComment,
		types.TypeDoBlock,
		types.TypeType,
		types.TypeDomain,
		types.TypeEnum,
		types.TypeComposite,
		types.TypeSubscription,
		types.TypeStatistic,
		types.TypeGeneratedColumn,
		types.TypeMultirangeType,
		types.TypeVectorIndex,
		types.TypeEventTrigger,
		types.TypeUnknown,
	}
}

// IsValidObjectType checks if an ObjectType is valid
func IsValidObjectType(t types.ObjectType) bool {
	switch t {
	case types.TypeTable,
		types.TypeIndex,
		types.TypeFunction,
		types.TypeTrigger,
		types.TypeView,
		types.TypeSequence,
		types.TypeConstraint,
		types.TypePolicy,
		types.TypeRole,
		types.TypeSchema,
		types.TypeExtension,
		types.TypePublication,
		types.TypeComment,
		types.TypeDoBlock,
		types.TypeType,
		types.TypeDomain,
		types.TypeEnum,
		types.TypeComposite,
		types.TypeSubscription,
		types.TypeStatistic,
		types.TypeGeneratedColumn,
		types.TypeMultirangeType,
		types.TypeVectorIndex,
		types.TypeEventTrigger,
		types.TypeUnknown:
		return true
	default:
		return false
	}
}

// ValidateObjectType returns an error if the ObjectType is invalid
func ValidateObjectType(t types.ObjectType) error {
	if !IsValidObjectType(t) {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, fmt.Sprintf("invalid object type: %s", t), nil)
	}
	return nil
}

// Operation validation

// ValidOperations returns all valid Operation values
func ValidOperations() []types.Operation {
	return []types.Operation{
		types.OpCreate,
		types.OpAlter,
		types.OpDrop,
		types.OpInsert,
		types.OpUpdate,
		types.OpDelete,
		types.OpGrant,
		types.OpRevoke,
		types.OpComment,
	}
}

// IsValidOperation checks if an Operation is valid
func IsValidOperation(op types.Operation) bool {
	switch op {
	case types.OpCreate,
		types.OpAlter,
		types.OpDrop,
		types.OpInsert,
		types.OpUpdate,
		types.OpDelete,
		types.OpGrant,
		types.OpRevoke,
		types.OpComment:
		return true
	default:
		return false
	}
}

// ValidateOperation returns an error if the Operation is invalid
func ValidateOperation(op types.Operation) error {
	if !IsValidOperation(op) {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, fmt.Sprintf("invalid operation: %s", op), nil)
	}
	return nil
}

// Category validation

// ValidCategories returns all valid Category values
func ValidCategories() []types.Category {
	return []types.Category{
		types.CategoryFoundation,
		types.CategoryConstraints,
		types.CategoryIndexes,
		types.CategoryFunctions,
		types.CategoryTriggers,
		types.CategorySecurity,
		types.CategoryData,
		types.CategoryExtensions,
		types.CategoryCritical,
	}
}

// IsValidCategory checks if a Category is valid
func IsValidCategory(c types.Category) bool {
	switch c {
	case types.CategoryFoundation,
		types.CategoryConstraints,
		types.CategoryIndexes,
		types.CategoryFunctions,
		types.CategoryTriggers,
		types.CategorySecurity,
		types.CategoryData,
		types.CategoryExtensions,
		types.CategoryCritical:
		return true
	default:
		return false
	}
}

// ValidateCategory returns an error if the Category is invalid
func ValidateCategory(c types.Category) error {
	if !IsValidCategory(c) {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, fmt.Sprintf("invalid category: %s", c), nil)
	}
	return nil
}

// IsDDLOperation checks if an operation is a DDL operation (CREATE, ALTER, DROP)
func IsDDLOperation(op types.Operation) bool {
	switch op {
	case types.OpCreate, types.OpAlter, types.OpDrop:
		return true
	default:
		return false
	}
}

// IsDMLOperation checks if an operation is a DML operation (INSERT, UPDATE, DELETE)
func IsDMLOperation(op types.Operation) bool {
	switch op {
	case types.OpInsert, types.OpUpdate, types.OpDelete:
		return true
	default:
		return false
	}
}

// IsSecurityOperation checks if an operation is a security operation (GRANT, REVOKE)
func IsSecurityOperation(op types.Operation) bool {
	switch op {
	case types.OpGrant, types.OpRevoke:
		return true
	default:
		return false
	}
}
