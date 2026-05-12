ALTER TABLE nodes
    ADD COLUMN region_code VARCHAR(16) NOT NULL DEFAULT '' AFTER name,
    ADD COLUMN region_name VARCHAR(64) NOT NULL DEFAULT '' AFTER region_code,
    ADD COLUMN region_flag VARCHAR(16) NOT NULL DEFAULT '' AFTER region_name;
