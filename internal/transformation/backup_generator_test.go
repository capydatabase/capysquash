package transformation

import (
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestGenerateAlterTableRollback(t *testing.T) {
	t.Parallel()

	bg := NewBackupGenerator(nil, nil)

	tests := []struct {
		name string
		stmt types.Statement
		want string
	}{
		{
			name: "add column",
			stmt: types.Statement{
				ObjectName: "public.users",
				SQL:        "ALTER TABLE public.users ADD COLUMN age integer;",
			},
			want: "ALTER TABLE public.users DROP COLUMN IF EXISTS age;",
		},
		{
			name: "drop constraint",
			stmt: types.Statement{
				ObjectName: "public.users",
				SQL:        "ALTER TABLE public.users DROP CONSTRAINT users_email_key;",
			},
			want: "-- Rollback DROP CONSTRAINT users_email_key: requires constraint definition from backup",
		},
		{
			name: "set not null",
			stmt: types.Statement{
				ObjectName: "public.users",
				SQL:        "ALTER TABLE public.users ALTER COLUMN email SET NOT NULL;",
			},
			want: "ALTER TABLE public.users ALTER COLUMN email DROP NOT NULL;",
		},
		{
			name: "rename column",
			stmt: types.Statement{
				ObjectName: "public.users",
				SQL:        "ALTER TABLE public.users RENAME COLUMN old_name TO new_name;",
			},
			want: "ALTER TABLE public.users RENAME COLUMN new_name TO old_name;",
		},
		{
			name: "enable trigger",
			stmt: types.Statement{
				ObjectName: "public.users",
				SQL:        "ALTER TABLE public.users ENABLE TRIGGER users_sync_trg;",
			},
			want: "ALTER TABLE public.users DISABLE TRIGGER users_sync_trg;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := bg.generateAlterTableRollback(tc.stmt)
			assert.Equal(t, tc.want, got)
		})
	}
}
