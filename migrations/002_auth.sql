-- Auth: passwordless sign-in creates accounts without a password or display name,
-- and every account tracks whether its email has been verified (SRS 4.1).
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ALTER COLUMN display_name SET DEFAULT '';
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- One active code per email and purpose; requesting a new one replaces the old one.
CREATE TABLE email_codes (
  email TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('sign_in')),
  code_hash TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  expires_at TIMESTAMPTZ NOT NULL,
  resend_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (email, purpose)
);
