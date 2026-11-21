-- 0011: WebShell 目标侧物理路径（P1-7）。
-- 记录上传马体在目标机上的绝对路径，清理时先 rm 再校验，避免只删会话留下文件。
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS artifact_path TEXT NOT NULL DEFAULT '';
