-- Phase 2: tasks, tags, checklist items and completion records (SPEC
-- section 4), the purge watermark of the sync cursor (D-21) and the
-- extension behind accent-insensitive search (D-42).

-- +goose Up

-- unaccent is a "trusted" extension: the owner of the database may create
-- it without being a superuser (SPEC section 10).
CREATE EXTENSION IF NOT EXISTS unaccent;

-- The highest seq the purge (phase 6) has removed. A sync cursor below it
-- may have missed tombstones and answers 410 (D-21).
ALTER TABLE sync_state
    ADD COLUMN purged_up_to_seq bigint NOT NULL DEFAULT 0 CHECK (purged_up_to_seq >= 0);

CREATE TABLE tags (
    id         uuid        PRIMARY KEY,
    -- Tags belong to a user, not to a list.
    owner_id   uuid        NOT NULL REFERENCES users (id),
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    color      text        CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    version    integer     NOT NULL CHECK (version >= 1),
    seq        bigint      NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz
);

-- A live tag name is unique per user, ignoring case (SPEC section 4). A
-- deleted tag does not block its name.
CREATE UNIQUE INDEX tags_live_name ON tags (owner_id, lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX tags_seq ON tags (seq);

CREATE TABLE tasks (
    id                    uuid        PRIMARY KEY,
    list_id               uuid        NOT NULL REFERENCES lists (id),
    title                 text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 500),
    description           text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 20000),
    status                text        NOT NULL CHECK (status IN ('open', 'done', 'dropped')),
    priority              smallint    NOT NULL CHECK (priority BETWEEN 0 AND 3),
    position              text        NOT NULL CHECK (char_length(position) BETWEEN 1 AND 100),
    due_date              date,
    due_time              time,
    due_tz                text,
    rrule                 text,
    repeat_from           text        NOT NULL CHECK (repeat_from IN ('due', 'completion')),
    recurrence_done_count integer     NOT NULL DEFAULT 0 CHECK (recurrence_done_count >= 0),
    completed_at          timestamptz,
    -- Tags sync as a field of the task (SPEC section 4).
    tag_ids               uuid[]      NOT NULL DEFAULT '{}',
    -- Set when the task was deleted by deleting its list, so restoring the
    -- list restores exactly these tasks (D-20).
    deleted_with_list_id  uuid        REFERENCES lists (id),
    version               integer     NOT NULL CHECK (version >= 1),
    seq                   bigint      NOT NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    deleted_at            timestamptz,
    -- The dependencies between due fields (SPEC section 4, D-26).
    CONSTRAINT tasks_due_time_needs_date CHECK (due_time IS NULL OR due_date IS NOT NULL),
    CONSTRAINT tasks_due_tz_needs_time   CHECK (due_tz IS NULL OR due_time IS NOT NULL),
    CONSTRAINT tasks_rrule_needs_date    CHECK (rrule IS NULL OR due_date IS NOT NULL),
    CONSTRAINT tasks_completed_when_done CHECK ((status = 'done') = (completed_at IS NOT NULL)),
    CONSTRAINT tasks_deleted_with_list   CHECK (deleted_with_list_id IS NULL OR deleted_at IS NOT NULL)
);

CREATE INDEX tasks_list ON tasks (list_id);
CREATE UNIQUE INDEX tasks_seq ON tasks (seq);
-- The tag filter asks "does tag_ids contain X"; a GIN index answers that.
CREATE INDEX tasks_tag_ids ON tasks USING gin (tag_ids);

CREATE TABLE checklist_items (
    id         uuid        PRIMARY KEY,
    task_id    uuid        NOT NULL REFERENCES tasks (id),
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 500),
    is_done    boolean     NOT NULL DEFAULT false,
    position   text        NOT NULL CHECK (char_length(position) BETWEEN 1 AND 100),
    version    integer     NOT NULL CHECK (version >= 1),
    seq        bigint      NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz
);

CREATE INDEX checklist_items_task ON checklist_items (task_id);
CREATE UNIQUE INDEX checklist_items_seq ON checklist_items (seq);

-- The completion log (D-09). Not syncable (D-33), so no version or
-- deleted_at; undoing sets undone_at (D-39).
CREATE TABLE task_completions (
    id                         uuid        PRIMARY KEY,
    task_id                    uuid        NOT NULL REFERENCES tasks (id),
    kind                       text        NOT NULL CHECK (kind IN ('completed', 'skipped')),
    occurrence_due_date        date,
    completed_at               timestamptz NOT NULL,
    -- The task's state before this record, restored by uncomplete.
    prev_due_date              date,
    prev_due_time              time,
    prev_recurrence_done_count integer     NOT NULL,
    prev_status                text        NOT NULL CHECK (prev_status IN ('open', 'done', 'dropped')),
    -- The seq the task took when this record was written. It orders the
    -- records of a task: the latest one is the only one that can be undone.
    -- completed_at cannot do it, because the client may backdate it.
    task_seq                   bigint      NOT NULL,
    undone_at                  timestamptz
);

CREATE INDEX task_completions_task ON task_completions (task_id, task_seq);
CREATE INDEX task_completions_completed_at ON task_completions (completed_at);
