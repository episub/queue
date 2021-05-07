CREATE TABLE public.message_queue
(
    message_queue_id     uuid        NOT NULL DEFAULT gen_random_uuid(),
    data                 jsonb       NOT NULL DEFAULT '{}',
    task_key             varchar(64) NOT NULL,
    task_name            varchar(64) NOT NULL,
    created_at           timestamptz          DEFAULT Now(),
    created_by           varchar(64) NOT NULL,
    last_attempted       timestamptz NOT NULL DEFAULT Now(),
    state                varchar(16) NOT NULL,
    last_attempt_message varchar     NOT NULL,
    do_after             timestamptz NOT NULL DEFAULT Now(),
    CONSTRAINT message_queue_id_pk PRIMARY KEY (message_queue_id)
);

CREATE TABLE public.cdc_hash
(
    cdc_hash_id       uuid        NOT NULL DEFAULT gen_random_uuid(),
    cdc_controller_id uuid        NOT NULL,
    object_id         varchar     NOT NULL,
    hash              uuid,
    created_at        timestamptz NOT NULL DEFAULT Now(),
    updated_at        timestamptz NOT NULL DEFAULT Now(),
    CONSTRAINT cdc_hash_pk PRIMARY KEY (cdc_hash_id),
    CONSTRAINT cdc_hash_controller_object_uq UNIQUE (cdc_controller_id, object_id)
);

CREATE INDEX idx_cdc_hash_id ON public.cdc_hash (cdc_controller_id, object_id);
CREATE INDEX idx_cdc_hash_id_hash ON public.cdc_hash (cdc_controller_id, object_id, hash);
