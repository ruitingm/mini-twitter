CREATE TABLE IF NOT EXISTS tweets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content     VARCHAR(280) NOT NULL,
    like_count  INT NOT NULL DEFAULT 0,
    reply_to_id UUID REFERENCES tweets(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tweets_user_id_created ON tweets(user_id, created_at DESC);
