-- The host an egress request is about (design §4.2, D38). It has its own column
-- and is never parsed from the subject or the input: a host name the reader
-- validated, set only for a Decision with the cause egress_request.
ALTER TABLE decisions ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
