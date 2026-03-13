CREATE TABLE IF NOT EXISTS likes (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tweet_id   UUID NOT NULL REFERENCES tweets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, tweet_id)
);
CREATE INDEX IF NOT EXISTS idx_likes_tweet ON likes(tweet_id);

CREATE TABLE IF NOT EXISTS like_count_pending (
    tweet_id   UUID NOT NULL REFERENCES tweets(id) ON DELETE CASCADE,
    delta      INT  NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
