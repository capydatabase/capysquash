## Plan: E2E Migration Consolidation Validation

Conduct comprehensive end-to-end testing of pgsquash-engine's ability to consolidate the 15 production migrations (3,000+ lines) into optimized, production-ready output across all safety levels, with and without AI features, using Docker-based schema validation with zero-tolerance success criteria.

**Steps:**

1. **Establish baseline and test matrix** - Document current migrations object inventory (tables, indexes, functions, triggers, constraints, RLS policies), create 8 test configuration files in `test-configs/` (paranoid/conservative/standard/aggressive × AI-enabled/AI-disabled with Azure OpenAI), verify Docker and add TODO for paranoid mode production DSN support in engine.go

2. **Execute squash operations across all scenarios** - Run `pgsquash squash migrations/*.sql --safety-level <level> --output squashed/<scenario>/` for each configuration, capture consolidation metrics (file reduction, statement counts, object preservation), log any warnings from squasher and track which objects were consolidated vs preserved

3. **Perform Docker-based validation with zero-tolerance checks** - Execute `scripts/validate.sh --mode full` for each scenario using TWO_DATABASES mode, compare original vs squashed object counts (must be identical), verify object definitions match (excluding order/whitespace), fail validation if any unique object is missing or schema semantics differ

4. **Cross-validate with alternative validation modes and PostgreSQL versions** - Re-run best-performing scenarios with TWO_CONTAINERS and SCHEMA_DIFF modes to verify consistency, test against PostgreSQL 13/15/17 via docker-compose.testing.yml, ensure no version-specific failures or object count discrepancies

5. **Analyze AI impact and consolidation effectiveness** - Diff AI-enabled vs AI-disabled outputs for each safety level, measure actual consolidation ratios against targets (conservative 20-35%, standard 35-50%, aggressive 50-70%), assess manager.go contributions to dead code removal and function deduplication accuracy

6. **Document bugs and production-readiness verdict** - Create bug report for any validation failures, missing objects, incorrect consolidations, or Docker issues that block zero-tolerance criteria, produce final assessment identifying which safety level(s) achieve production-ready status (complete schema preservation + optimal consolidation), recommend deployment-ready configuration

**Implementation Notes:**

- **Zero-tolerance validation**: Schema comparison must show identical unique object counts and semantically equivalent definitions; any missing table, index, function, trigger, constraint, or RLS policy constitutes a bug
- **Mock paranoid mode**: Test paranoid level without production DSN, add TODO comment in relevant code for future DSN integration
- **Azure OpenAI default**: Configure `ai.provider: "azure-openai"` in test configs, document API key requirement in test setup
- **Bug criteria**: Any deviation from zero-tolerance (missing objects, schema differences, validation failures) or blocking issues (Docker errors, parse failures, consolidation errors)