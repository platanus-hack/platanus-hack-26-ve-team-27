-- 0009_call_ficha: the context an LLM took from a finished call (#106): lugar, personas,
-- vehiculo, rumbo, situacion and confianza, each "no se sabe" when the call did not say it.
-- Set with the verdict on duress and unclear calls; NULL on no_duress, on older calls and
-- when the extraction failed. The family's WhatsApp, the report and the central read it.
ALTER TABLE calls ADD COLUMN ficha JSONB;
