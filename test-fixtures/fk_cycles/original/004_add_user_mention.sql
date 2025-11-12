-- Create user_mentions table with FKs that complete the cycle
-- This creates: posts -> comments -> post_reactions -> posts (cycle)
CREATE TABLE user_mentions (
    id SERIAL PRIMARY KEY,
    post_id INTEGER REFERENCES posts(id) ON DELETE CASCADE,
    comment_id INTEGER REFERENCES comments(id) ON DELETE CASCADE,
    reaction_id INTEGER REFERENCES post_reactions(id) ON DELETE CASCADE,
    mentioned_user_id INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);
