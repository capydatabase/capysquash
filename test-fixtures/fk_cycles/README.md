# Test fixture: fk_cycles
# Tests circular foreign key detection and resolution

This fixture tests how pgsquash handles circular foreign key dependencies:

1. **Detection**: Should detect circular FK relationships
2. **Resolution**: Should use 2-phase approach to resolve cycles
3. **Validation**: Should ensure final schema is equivalent

## Original migrations:

001_create_posts.sql: Creates posts table
002_create_comments.sql: Creates comments table with FK to posts
003_create_post_reactions.sql: Creates post_reactions table with FK to posts and comments
004_add_user_mention.sql: Creates user_mentions table with FKs to multiple tables (creates cycle)

## Expected behavior:

- Should detect the circular dependency: posts -> comments -> post_reactions -> posts
- Should resolve using 2-phase approach (temporary disable constraints)
- Final output should maintain all relationships
