package validation

import (
	"testing"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
)

func TestObjectID_QualifiedName(t *testing.T) {
	tests := []struct {
		name     string
		objectID ObjectID
		want     string
	}{
		{
			name: "public schema function",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "public",
				Name:   "my_function",
			},
			want: "my_function",
		},
		{
			name: "auth schema function",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "auth",
				Name:   "jwt",
			},
			want: "auth.jwt",
		},
		{
			name: "custom schema function",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "custom",
				Name:   "process_data",
			},
			want: "custom.process_data",
		},
		{
			name: "function without schema",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "",
				Name:   "standalone_func",
			},
			want: "standalone_func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.objectID.QualifiedName()
			if got != tt.want {
				t.Errorf("ObjectID.QualifiedName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectID_String(t *testing.T) {
	tests := []struct {
		name     string
		objectID ObjectID
		want     string
	}{
		{
			name: "public schema function",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "public",
				Name:   "my_function",
			},
			want: "my_function::FUNCTION",
		},
		{
			name: "auth schema function",
			objectID: ObjectID{
				Type:   types.TypeFunction,
				Schema: "auth",
				Name:   "jwt",
			},
			want: "auth.jwt::FUNCTION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.objectID.String()
			if got != tt.want {
				t.Errorf("ObjectID.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFunctionChanges_Description(t *testing.T) {
	authJWT := ObjectID{
		Type:   types.TypeFunction,
		Schema: "auth",
		Name:   "jwt",
	}

	publicFunc := ObjectID{
		Type:   types.TypeFunction,
		Schema: "public",
		Name:   "my_func",
	}

	tests := []struct {
		name   string
		change SchemaChange
		want   string
	}{
		{
			name: "create auth.jwt function",
			change: &FunctionCreateChange{
				ID: authJWT,
				Definition: &FunctionDefinition{
					ID:         authJWT,
					Parameters: []Parameter{},
					ReturnType: "jsonb",
					Language:   "plpgsql",
				},
			},
			want: "Create function auth.jwt",
		},
		{
			name: "drop auth.jwt function",
			change: &FunctionDropChange{
				ID: authJWT,
				Definition: &FunctionDefinition{
					ID:         authJWT,
					Parameters: []Parameter{},
					ReturnType: "jsonb",
					Language:   "plpgsql",
				},
			},
			want: "Drop function auth.jwt",
		},
		{
			name: "recreate public function",
			change: &FunctionModifyChange{
				ID: publicFunc,
				FromDefinition: &FunctionDefinition{
					ID:         publicFunc,
					Parameters: []Parameter{},
					ReturnType: "text",
					Language:   "sql",
				},
				ToDefinition: &FunctionDefinition{
					ID:         publicFunc,
					Parameters: []Parameter{},
					ReturnType: "text",
					Language:   "plpgsql",
				},
			},
			want: "Recreate function my_func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.change.Description()
			if got != tt.want {
				t.Errorf("Description() = %v, want %v", got, tt.want)
			}
		})
	}
}
