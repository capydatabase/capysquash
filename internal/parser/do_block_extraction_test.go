package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAlterStatementsFromDoBlock_StaticDDL(t *testing.T) {
	t.Parallel()

	doBlock := `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'widgets' AND column_name = 'color') THEN
    ALTER TABLE public.widgets ADD COLUMN color TEXT;
    CREATE INDEX idx_widgets_color ON public.widgets(color);
    CREATE TYPE public.widget_state AS ENUM ('draft', 'ready');
  END IF;
END $$;
`

	statements := extractAlterStatementsFromDoBlock(doBlock)
	require.Len(t, statements, 3)

	assert.Contains(t, statements, "ALTER TABLE public.widgets ADD COLUMN color TEXT;")
	assert.Contains(t, statements, "CREATE INDEX idx_widgets_color ON public.widgets(color);")
	assert.Contains(t, statements, "CREATE TYPE public.widget_state AS ENUM ('draft', 'ready');")
}

func TestExtractAlterStatementsFromDoBlock_SkipsDynamicExecute(t *testing.T) {
	t.Parallel()

	doBlock := `
DO $$
BEGIN
  EXECUTE format('ALTER TABLE %I ADD COLUMN color TEXT', 'widgets');
  EXECUTE format('CREATE INDEX %I ON widgets(color)', 'idx_widgets_color');
END $$;
`

	statements := extractAlterStatementsFromDoBlock(doBlock)
	assert.Empty(t, statements)
}

func TestExtractAlterStatementsFromDoBlock_TaggedDollarBody(t *testing.T) {
	t.Parallel()

	doBlock := `
DO $body$
BEGIN
  ALTER TABLE widgets ADD COLUMN archived_at TIMESTAMPTZ;
END
$body$;
`

	statements := extractAlterStatementsFromDoBlock(doBlock)
	require.Len(t, statements, 1)
	assert.Equal(t, "ALTER TABLE widgets ADD COLUMN archived_at TIMESTAMPTZ;", statements[0])
}
