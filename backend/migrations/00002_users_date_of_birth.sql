-- +goose Up
-- Ngay sinh cua sinh vien (31/10/2026). Import sinh vien bat buoc co ngay sinh
-- va mat khau ban dau = ngay sinh dang ddmmyyyy. Cot cho phep NULL vi giao vien
-- va sinh vien tao truoc migration nay khong co ngay sinh.
ALTER TABLE users ADD COLUMN date_of_birth date;

-- +goose Down
ALTER TABLE users DROP COLUMN date_of_birth;
