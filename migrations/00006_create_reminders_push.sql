-- Phase 5: reminders, Web Push subscriptions and notification deliveries
-- (SPEC sections 4 and 6).

-- +goose Up

CREATE TABLE reminders (
    id             uuid        PRIMARY KEY,
    task_id        uuid        NOT NULL REFERENCES tasks (id),
    kind           text        NOT NULL CHECK (kind IN ('relative', 'absolute', 'snooze')),
    -- relative: minutes before the due instant (SPEC section 6).
    offset_minutes integer     CHECK (offset_minutes BETWEEN 0 AND 40320),
    -- absolute and snooze: the instant itself.
    at             timestamptz,
    -- Computed by the server; null when nothing is pending.
    next_fire_at   timestamptz,
    last_fired_at  timestamptz,
    version        integer     NOT NULL CHECK (version >= 1),
    seq            bigint      NOT NULL,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    deleted_at     timestamptz,
    CONSTRAINT reminders_kind_fields CHECK (
        (kind = 'relative' AND offset_minutes IS NOT NULL AND at IS NULL)
        OR (kind IN ('absolute', 'snooze') AND at IS NOT NULL AND offset_minutes IS NULL)
    )
);

CREATE INDEX reminders_task ON reminders (task_id);
CREATE UNIQUE INDEX reminders_seq ON reminders (seq);
-- The scheduler's poll: pending reminders by due instant.
CREATE INDEX reminders_pending ON reminders (next_fire_at)
    WHERE next_fire_at IS NOT NULL AND deleted_at IS NULL;

-- Not syncable: deleting one is a hard delete (D-32).
CREATE TABLE push_subscriptions (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id),
    channel         text        NOT NULL CHECK (channel = 'webpush'),
    -- A push service URL names one browser installation; it belongs to one
    -- user at a time (D-32).
    endpoint        text        NOT NULL UNIQUE CHECK (char_length(endpoint) <= 2000),
    p256dh          text        NOT NULL,
    auth            text        NOT NULL,
    label           text        CHECK (char_length(label) <= 100),
    created_at      timestamptz NOT NULL,
    last_success_at timestamptz,
    disabled_at     timestamptz
);

CREATE INDEX push_subscriptions_user ON push_subscriptions (user_id);

CREATE TABLE notification_deliveries (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    reminder_id     uuid        NOT NULL REFERENCES reminders (id),
    -- The reminder's next_fire_at when it fired.
    fire_at         timestamptz NOT NULL,
    -- A deleted subscription takes its deliveries with it.
    subscription_id uuid        NOT NULL REFERENCES push_subscriptions (id) ON DELETE CASCADE,
    status          text        NOT NULL CHECK (status IN ('pending', 'sent', 'failed', 'skipped')),
    attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    -- When a pending delivery is tried next (D-66).
    next_attempt_at timestamptz,
    sent_at         timestamptz,
    -- What the push service answered; never the payload.
    error           text,
    created_at      timestamptz NOT NULL,
    -- Nothing is sent twice for one firing (SPEC section 4).
    CONSTRAINT notification_deliveries_once UNIQUE (reminder_id, fire_at, subscription_id),
    CONSTRAINT notification_deliveries_pending_has_time CHECK (status <> 'pending' OR next_attempt_at IS NOT NULL)
);

CREATE INDEX notification_deliveries_due ON notification_deliveries (next_attempt_at)
    WHERE status = 'pending';
