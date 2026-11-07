CREATE TABLE support_cases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind TEXT NOT NULL CHECK (kind IN ('attendee', 'organizer')),
  requester_id UUID NOT NULL REFERENCES users(id),
  organizer_id UUID NOT NULL REFERENCES organizers(id),
  event_id UUID REFERENCES events(id),
  order_id UUID REFERENCES orders(id),
  ticket_id UUID REFERENCES tickets(id),
  category TEXT NOT NULL CHECK (category IN ('ticket_delivery', 'payment', 'refund', 'seating',
    'event_information', 'check_in', 'account', 'activation', 'technical', 'other')),
  subject TEXT NOT NULL CHECK (btrim(subject) <> ''),
  status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'in_progress', 'waiting_for_customer', 'resolved')),
  assignee_id UUID REFERENCES users(id),
  last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_activity_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_support_cases_requester ON support_cases (requester_id, last_activity_at DESC);
CREATE INDEX idx_support_cases_organizer ON support_cases (organizer_id, last_activity_at DESC) WHERE kind = 'attendee';
CREATE INDEX idx_support_cases_activity ON support_cases (last_activity_at DESC);

CREATE TABLE support_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  case_id UUID NOT NULL REFERENCES support_cases(id),
  kind TEXT NOT NULL CHECK (kind IN ('message', 'status_change', 'assignment')),
  author_id UUID NOT NULL REFERENCES users(id),
  body TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_support_messages_case ON support_messages (case_id, created_at);

CREATE TABLE support_case_reads (
  case_id UUID NOT NULL REFERENCES support_cases(id),
  user_id UUID NOT NULL REFERENCES users(id),
  last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (case_id, user_id)
);
