-- Create posts table
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    content TEXT,
    author_id INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create comments table with FK to posts
CREATE TABLE comments (
    id SERIAL PRIMARY KEY,
    post_id INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create post_reactions table with FKs that create a cycle
CREATE TABLE post_reactions (
    id SERIAL PRIMARY KEY,
    post_id INTEGER NOT NULL,
    comment_id INTEGER,
    reaction_type VARCHAR(20) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create user_mentions table with FKs that complete the cycle
-- This creates: posts -> comments -> post_reactions -> posts (cycle)
CREATE TABLE user_mentions (
    id SERIAL PRIMARY KEY,
    post_id INTEGER,
    comment_id INTEGER,
    reaction_id INTEGER,
    mentioned_user_id INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Add foreign key constraints (2-phase approach to resolve cycles)
-- Phase 1: Add constraints that don't create cycles
ALTER TABLE comments ADD CONSTRAINT fk_comments_post_id
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE;

ALTER TABLE post_reactions ADD CONSTRAINT fk_post_reactions_post_id
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE;

-- Phase 2: Add constraints that complete cycles (after all tables exist)
ALTER TABLE post_reactions ADD CONSTRAINT fk_post_reactions_comment_id
    FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE;

ALTER TABLE user_mentions ADD CONSTRAINT fk_user_mentions_post_id
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE;

ALTER TABLE user_mentions ADD CONSTRAINT fk_user_mentions_comment_id
    FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE;

ALTER TABLE user_mentions ADD CONSTRAINT fk_user_mentions_reaction_id
    FOREIGN KEY (reaction_id) REFERENCES post_reactions(id) ON DELETE CASCADE;
