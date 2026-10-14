-- Mail outbox for async SMTP delivery

CREATE TABLE mail_outbox (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    kind VARCHAR(32) NOT NULL,
    to_email VARCHAR(255) NOT NULL,
    subject VARCHAR(200) NOT NULL,
    body TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    last_error TEXT,
    available_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_mail_outbox_poll ON mail_outbox (status, available_at);
