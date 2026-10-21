ALTER TABLE pages ADD COLUMN protection_level TEXT NOT NULL DEFAULT 'none';
UPDATE pages SET protection_level = 'semi' WHERE protected = true;
ALTER TABLE pages ADD CONSTRAINT pages_protection_level_check CHECK (protection_level IN ('none', 'semi', 'full'));
