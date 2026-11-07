ALTER TABLE paid_sales_activations DROP CONSTRAINT paid_sales_activations_status_check;
ALTER TABLE paid_sales_activations ADD CONSTRAINT paid_sales_activations_status_check
  CHECK (status IN ('pending', 'active', 'suspended', 'rejected'));
ALTER TABLE paid_sales_activations ADD COLUMN review_note TEXT NOT NULL DEFAULT '';
ALTER TABLE paid_sales_activations ADD COLUMN requested_at TIMESTAMPTZ;
UPDATE paid_sales_activations SET requested_at = COALESCE(activated_at, created_at);

ALTER TABLE events ADD COLUMN image_urls TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE payouts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL UNIQUE REFERENCES events(id),
  organizer_id UUID NOT NULL REFERENCES organizers(id),
  gross_amount BIGINT NOT NULL CHECK (gross_amount >= 0),
  refunded_amount BIGINT NOT NULL CHECK (refunded_amount >= 0),
  fee_amount BIGINT NOT NULL CHECK (fee_amount >= 0),
  amount BIGINT NOT NULL CHECK (amount >= 0),
  payout_account_reference TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'on_hold', 'paid')),
  provider_reference TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  paid_at TIMESTAMPTZ
);
CREATE INDEX idx_payouts_status ON payouts (status, created_at DESC);
CREATE INDEX idx_payouts_organizer ON payouts (organizer_id, created_at DESC);
