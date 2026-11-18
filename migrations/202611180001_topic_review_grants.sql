-- +goose Up
CREATE TABLE topic_review_grants (
    topic_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (topic_id, user_id)
);

CREATE INDEX topic_review_grants_user_idx ON topic_review_grants(user_id);

-- +goose Down
DROP TABLE topic_review_grants;
