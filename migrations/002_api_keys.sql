CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100),
    audience VARCHAR(100) NOT NULL,
    audience_type VARCHAR(20) NOT NULL DEFAULT 'text',
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMP WITH TIME ZONE,
    revoked_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_api_keys_owner_user_id ON api_keys(owner_user_id);
CREATE INDEX idx_api_keys_audience ON api_keys(audience);
