-- Organizer details are scoped to one account. A payout reference is a demo label,
-- not a bank account number or payment credential.
ALTER TABLE organizers ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE organizers ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
ALTER TABLE organizers ADD COLUMN contact_phone TEXT NOT NULL DEFAULT '';
ALTER TABLE organizers ADD COLUMN payout_account_reference TEXT NOT NULL DEFAULT '';

ALTER TABLE events ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN venue_name TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN venue_address TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN timezone TEXT NOT NULL DEFAULT 'Asia/Almaty';
ALTER TABLE events ADD COLUMN visibility TEXT NOT NULL DEFAULT 'public'
  CHECK (visibility IN ('public', 'unlisted', 'private'));
ALTER TABLE events ADD COLUMN registration_opens_at TIMESTAMPTZ;
ALTER TABLE events ADD COLUMN registration_closes_at TIMESTAMPTZ;
ALTER TABLE events ADD COLUMN capacity INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0);
ALTER TABLE events ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE events ADD CONSTRAINT events_registration_window CHECK (
  registration_opens_at IS NULL OR registration_closes_at IS NULL
  OR registration_closes_at > registration_opens_at
);

-- Keep the original demo event's capacity aligned with its seeded 100-ticket inventory.
UPDATE events SET capacity = 100
WHERE id = '00000000-0000-0000-0000-000000000010';
