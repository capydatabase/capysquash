package consolidation

import (
	"testing"

	"github.com/capy-base/pgsquash-engine/internal/config"
	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopConsolidationEngine struct{}

func (noopConsolidationEngine) GetTracker() *tracking.Tracker { return nil }
func (noopConsolidationEngine) GetConfig() *config.Config     { return nil }
func (noopConsolidationEngine) GetSafetyLevel() string        { return "standard" }

func TestExtractAlterStatementsFromDoBlock_ParserDriven(t *testing.T) {
	t.Parallel()

	doBlock := `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'widgets' AND column_name = 'color') THEN
    ALTER TABLE public.widgets ADD COLUMN color TEXT;
    CREATE INDEX idx_widgets_color ON public.widgets(color);
  END IF;
END $$;`

	statements := extractAlterStatementsFromDoBlock(doBlock)
	require.Len(t, statements, 1)
	assert.Equal(t, "ALTER TABLE public.widgets ADD COLUMN color TEXT;", statements[0])
}

func TestDOBlockAlterTableRuleApply(t *testing.T) {
	t.Parallel()

	rule := &DOBlockAlterTableRule{}
	lifecycle := &tracking.ObjectLifecycle{
		Type: types.TypeDoBlock,
		History: []tracking.LifecycleEvent{
			{
				Statement: types.Statement{SQL: `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'widgets' AND column_name = 'color') THEN
    ALTER TABLE public.widgets ADD COLUMN color TEXT;
  END IF;
END $$;`},
			},
		},
	}

	result, err := rule.Apply(lifecycle, noopConsolidationEngine{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.ConsolidatedSQL, "ALTER TABLE public.widgets ADD COLUMN color TEXT;")
}
