-- cluster-services-1376: per-project opt-out for sandbox image auto-follow.
-- TRUE (the default) lets the controller move the project to the gateway's
-- RECOMMENDED_IMAGE_TAG, but only while the project has no active session.
-- FALSE pins the project on its current image_tag.
ALTER TABLE kubetty_projects
ADD COLUMN IF NOT EXISTS image_auto_follow BOOLEAN NOT NULL DEFAULT TRUE;
