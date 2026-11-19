-- Human decisions for checks the model placed in the review band.
CREATE TABLE IF NOT EXISTS reviews (
    check_id BIGINT PRIMARY KEY REFERENCES checks(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision IN ('allow', 'block')),
    note TEXT NOT NULL DEFAULT '',
    reviewer TEXT NOT NULL,
    decided_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reviews_decided ON reviews(decided_at DESC);
