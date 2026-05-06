ALTER TABLE expense_revisions DROP CONSTRAINT expense_revisions_field_check;
ALTER TABLE expense_revisions
    ADD CONSTRAINT expense_revisions_field_check
    CHECK (field IN ('description', 'amount_cents', 'category_id', 'payer_id', 'splits', 'incurred_at'));
