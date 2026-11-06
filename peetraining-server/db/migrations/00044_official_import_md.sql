-- T52 后台 7.17 资料导入支持 Markdown（.md）：团队把资料先整理成 md 再上传，不用再走 PDF 解析或拍照识别，省时也省钱。
-- 重复试题在打开核对区时实时比对，不需要新列。

-- +goose Up
ALTER TABLE official_import_files
  MODIFY COLUMN format ENUM('pdf','docx','image','text','csv','md') NOT NULL;

-- +goose Down
UPDATE official_import_files SET format = 'text' WHERE format = 'md';
ALTER TABLE official_import_files
  MODIFY COLUMN format ENUM('pdf','docx','image','text','csv') NOT NULL;
